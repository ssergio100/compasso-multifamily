package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ssergio100/compasso/server/storage"
)

// communicationDetails carries business context about an administrative
// request (bonus minutes, command, routine name...) from the handler to the
// logging middleware. Only non-sensitive values are stored; the storage
// validator rejects credential-like keys.
type communicationDetails struct {
	values     map[string]string
	authorized bool
	device     storage.FamilyDevice
}

type communicationDetailsKey struct{}

func communicationDetailsContext(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), communicationDetailsKey{}, &communicationDetails{values: map[string]string{}}))
}

func addCommunicationDetail(r *http.Request, key, value string) {
	if details, ok := r.Context().Value(communicationDetailsKey{}).(*communicationDetails); ok {
		details.values[key] = value
	}
}

func markAdministrativeCommunicationAuthorized(r *http.Request, device storage.FamilyDevice) {
	if details, ok := r.Context().Value(communicationDetailsKey{}).(*communicationDetails); ok {
		details.authorized = true
		details.device = device
	}
}

func administrativeCommunicationAuthorized(r *http.Request) bool {
	details, ok := r.Context().Value(communicationDetailsKey{}).(*communicationDetails)
	return ok && details.authorized
}

func extraCommunicationDetails(r *http.Request) map[string]string {
	if details, ok := r.Context().Value(communicationDetailsKey{}).(*communicationDetails); ok {
		return details.values
	}
	return nil
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusCapturingResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusCapturingResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (a *App) logAdministrativeCommunication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, operation, route, shouldLog := administrativeCommunication(r)
		if !shouldLog {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		correlationID, _ := randomToken()
		if correlationID != "" {
			w.Header().Set("X-Compasso-Correlation-ID", correlationID)
		}
		recorder := &statusCapturingResponseWriter{ResponseWriter: w}
		wrapped := communicationDetailsContext(r)
		next.ServeHTTP(recorder, wrapped)
		if !administrativeCommunicationAuthorized(wrapped) {
			return
		}
		status := recorder.statusCode()
		detailValues := map[string]string{
			"correlation_id": correlationID,
			"method":         r.Method,
			"route":          route,
		}
		for key, value := range extraCommunicationDetails(wrapped) {
			detailValues[key] = value
		}
		communicationContext, _ := wrapped.Context().Value(communicationDetailsKey{}).(*communicationDetails)
		stored, err := communicationContext.device.AppendCommunicationLog(r.Context(), storage.CommunicationLog{
			Source: "interface", Target: "api", Operation: operation,
			Result: communicationResultForStatus(status), HTTPStatus: status,
			DurationMS: elapsedMilliseconds(started), Summary: administrativeCommunicationSummary(operation, status),
			Details: detailValues,
		}, a.now())
		if err != nil {
			return
		}
		a.publishAdministrativeCommunicationLog(communicationContext.device, stored)
		if r.Method != http.MethodGet && status >= http.StatusOK && status < http.StatusMultipleChoices {
			a.publishAdministrativeActivitiesChanged(communicationContext.device)
		}
	})
}

func administrativeCommunication(r *http.Request) (deviceID, operation, route string, ok bool) {
	const prefix = "/api/v1/admin/devices/"
	if r.Method == http.MethodOptions || !strings.HasPrefix(r.URL.Path, prefix) {
		return "", "", "", false
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", "", false
	}
	deviceID = parts[0]
	resource := "device"
	if len(parts) > 1 {
		resource = parts[1]
	}
	if resource == "communication" || resource == "stream" || resource == "activities" {
		return "", "", "", false
	}
	// Automatic reads support screen hydration and activity tracking. They are
	// not human actions and would otherwise drown the useful history in polling.
	if r.Method == http.MethodGet && (resource == "device" || resource == "status" || resource == "commands" || resource == "events") {
		return "", "", "", false
	}
	operation = r.Method + " " + resource
	route = prefix + "{device_id}"
	if resource != "device" {
		route += "/" + resource
	}
	if len(parts) > 2 {
		route += "/{resource_id}"
	}
	return deviceID, operation, route, true
}

func administrativeCommunicationSummary(operation string, status int) string {
	if status >= 400 {
		return "Solicitação da interface rejeitada pela API."
	}
	return "Solicitação da interface processada pela API: " + operation + "."
}

func communicationResultForStatus(status int) string {
	if status >= 500 {
		return "error"
	}
	if status >= 400 {
		return "warning"
	}
	return "success"
}

func elapsedMilliseconds(started time.Time) int64 {
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 1 {
		return 1
	}
	return elapsed
}

func (a *App) adminDeviceCommunicationAPI(
	w http.ResponseWriter,
	r *http.Request,
	current session,
	device storage.FamilyDevice,
	pathParts []string,
) {
	if len(pathParts) != 0 {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, afterID, ok := communicationQuery(w, r)
		if !ok {
			return
		}
		events, err := device.ListCommunicationLogs(r.Context(), afterID, limit)
		if !writeAdminReadError(w, err) {
			return
		}
		retentionDays, err := a.store.CommunicationRetentionDays(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "could not load communication settings")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"events": events, "retention_days": retentionDays,
		})
	case http.MethodDelete:
		if !requireAdminCSRF(w, r, current) {
			return
		}
		deleted, err := device.DeleteCommunicationLogs(r.Context())
		if !writeAdminReadError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"deleted": deleted})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func communicationQuery(w http.ResponseWriter, r *http.Request) (limit int, afterID int64, ok bool) {
	limit = 200
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 500 {
			writeJSONError(w, http.StatusBadRequest, "limit must be between 1 and 500")
			return 0, 0, false
		}
		limit = parsed
	}
	if value := r.URL.Query().Get("after"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			writeJSONError(w, http.StatusBadRequest, "after must be a non-negative integer")
			return 0, 0, false
		}
		afterID = parsed
	}
	return limit, afterID, true
}
