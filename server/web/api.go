package web

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	protocol "github.com/ssergio100/compasso/protocol/v1"
	"github.com/ssergio100/compasso/server/storage"
)

const deviceIDHeader = "X-Tempo-Device-ID"

func (a *App) heartbeat(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	deviceID := strings.TrimSpace(r.Header.Get(deviceIDHeader))
	correlationID, _ := randomToken()
	if correlationID != "" {
		w.Header().Set("X-Compasso-Correlation-ID", correlationID)
	}
	protocolVersion := strings.TrimSpace(r.Header.Get(protocol.VersionHeader))
	if protocolVersion == "" {
		protocolVersion = "1"
	}
	details := map[string]string{
		"correlation_id": correlationID, "protocol_version": protocolVersion,
		"route": protocol.HeartbeatPath,
	}
	recorder := &statusCapturingResponseWriter{ResponseWriter: w}
	a.handleHeartbeat(recorder, r, details)
	a.metrics.record(recorder.statusCode(), time.Since(started))
	if deviceID == "" {
		return
	}
	status := recorder.statusCode()
	result := communicationResultForStatus(status)
	summary := "Heartbeat processado e resposta enviada ao agente."
	if status >= 400 {
		summary = details["rejection_reason"]
		if summary == "" {
			summary = "O servidor recusou a atualização enviada pelo computador."
		}
	}
	if details["authenticated"] == "true" && (status >= 400 || heartbeatCarriedState(details)) {
		heartbeatLog, _ := a.store.AppendCommunicationLog(r.Context(), storage.CommunicationLog{
			DeviceID: deviceID, Source: "agent", Target: "api", Operation: "heartbeat",
			Result: result, HTTPStatus: status, DurationMS: elapsedMilliseconds(started), Summary: summary,
			Details: details,
		}, a.now())
		a.publishCommunicationLog(deviceID, heartbeatLog)
	}
	if status == http.StatusOK && heartbeatCarriedState(details) {
		responseLog, _ := a.store.AppendCommunicationLog(r.Context(), storage.CommunicationLog{
			DeviceID: deviceID, Source: "api", Target: "agent", Operation: "heartbeat_response",
			Result: "success", HTTPStatus: status, DurationMS: elapsedMilliseconds(started),
			Summary: "API enviou atualizações ou confirmações ao agente.", Details: details,
		}, a.now())
		a.publishCommunicationLog(deviceID, responseLog)
	}
}

func (a *App) handleHeartbeat(w http.ResponseWriter, r *http.Request, details map[string]string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.rateLimits.takeBucket("heartbeat:global", 50, 50, a.now()) {
		writeHeartbeatRateLimit(w)
		return
	}
	deviceID := strings.TrimSpace(r.Header.Get(deviceIDHeader))
	authorization := r.Header.Get("Authorization")
	ok := strings.HasPrefix(authorization, "Bearer ")
	token := strings.TrimPrefix(authorization, "Bearer ")
	if !ok || strings.ContainsAny(token, " \t\r\n") {
		if !a.rateLimits.takeBucket("heartbeat:invalid:"+requestNetwork(r), 10, 20, a.now()) {
			writeHeartbeatRateLimit(w)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSONErrorResponse(w, http.StatusUnauthorized, protocol.ErrorResponse{
			Error: "invalid device credentials", Code: "invalid_device_credentials",
		})
		return
	}
	if err := a.store.AuthenticateDevice(r.Context(), deviceID, token); err != nil {
		if errors.Is(err, storage.ErrFamilySuspended) {
			details["authenticated"] = "true"
			details["rejection_reason"] = "A família deste computador está suspensa."
			details["failure_stage"] = "autorizacao_da_familia"
			writeJSONErrorResponse(w, http.StatusForbidden, protocol.ErrorResponse{
				Error: "family suspended", Code: "family_suspended",
			})
			return
		}
		if !a.rateLimits.takeBucket("heartbeat:invalid:"+requestNetwork(r), 10, 20, a.now()) {
			writeHeartbeatRateLimit(w)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSONErrorResponse(w, http.StatusUnauthorized, protocol.ErrorResponse{
			Error: "invalid device credentials", Code: "invalid_device_credentials",
		})
		return
	}
	details["authenticated"] = "true"
	protocolVersion := strings.TrimSpace(r.Header.Get(protocol.VersionHeader))
	if protocolVersion == "" {
		protocolVersion = "1"
	}
	if protocolVersion != "1" && protocolVersion != protocol.CurrentProtocolVersion {
		writeJSONError(w, http.StatusBadRequest, "unsupported protocol version")
		return
	}
	hasInstallationIdentity := requestHasCapability(r, protocol.InstallationIdentityCapability)
	if a.requireInstallationIdentity && !hasInstallationIdentity {
		writeJSONErrorResponse(w, http.StatusUpgradeRequired, protocol.ErrorResponse{
			Error: "agent update required", Code: "agent_upgrade_required",
		})
		return
	}
	installationID := ""
	if hasInstallationIdentity {
		installationID = strings.TrimSpace(r.Header.Get(protocol.InstallationIDHeader))
		if !protocol.ValidInstallationID(installationID) {
			writeJSONErrorResponse(w, http.StatusBadRequest, protocol.ErrorResponse{
				Error: "invalid installation identity", Code: "invalid_installation_identity",
			})
			return
		}
		if err := a.store.CheckDeviceInstallation(r.Context(), deviceID, installationID); err != nil {
			if errors.Is(err, storage.ErrInstallationConflict) {
				writeJSONErrorResponse(w, http.StatusConflict, protocol.ErrorResponse{
					Error: "another installation is active", Code: "installation_conflict",
				})
				return
			}
			writeJSONError(w, http.StatusBadRequest, "invalid installation identity")
			return
		}
		if a.requireInstallationIdentity && !a.rateLimits.takeBucket("heartbeat:device:"+deviceID, 1, 2, a.now()) {
			writeHeartbeatRateLimit(w)
			return
		}
	}
	if protocolVersion == "1" {
		pendingBonus, err := a.store.HasPendingRemoteBonus(r.Context(), deviceID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "could not inspect pending operations")
			return
		}
		if pendingBonus {
			writeJSONErrorResponse(w, http.StatusUpgradeRequired, protocol.ErrorResponse{
				Error: "agent update required before applying pending bonus", Code: "agent_upgrade_required",
			})
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request protocol.HeartbeatRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid heartbeat payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "invalid heartbeat payload")
		return
	}
	details["local_date"] = request.LocalDate
	details["policy_revision"] = strconv.FormatInt(request.PolicyRevision, 10)
	details["control_revision"] = strconv.FormatInt(request.ControlRevision, 10)
	details["session_state_revision"] = strconv.FormatInt(request.SessionStateRevision, 10)
	details["used_seconds"] = strconv.FormatInt(request.SecondsUsed, 10)
	details["request_events"] = strconv.Itoa(len(request.Events))
	details["command_acknowledgements"] = strconv.Itoa(len(request.CommandAcks))
	heartbeatNow := a.now()
	nextInterval := a.heartbeatInterval
	if hasInstallationIdentity {
		nextInterval = adaptiveHeartbeatInterval(request.GraphicalSessionActive)
	}
	var response protocol.HeartbeatResponse
	var err error
	if installationID == "" {
		response, err = a.store.ReceiveHeartbeat(r.Context(), deviceID, request, heartbeatNow)
	} else {
		response, err = a.store.ReceiveHeartbeatForInstallation(
			r.Context(), deviceID, installationID, nextInterval, request, heartbeatNow,
		)
	}
	if err != nil {
		details["rejection_reason"] = humanHeartbeatRejection(err)
		details["failure_stage"] = "processamento_no_servidor"
		log.Printf("heartbeat rejected device_id=%s correlation_id=%s error=%v",
			deviceID, details["correlation_id"], err)
		status := http.StatusBadRequest
		var revisionError *storage.RevisionAheadError
		if errors.As(err, &revisionError) {
			status = http.StatusConflict
			writeJSONErrorResponse(w, status, protocol.ErrorResponse{
				Error:          "client synchronization state is newer than this device",
				Code:           "revision_ahead",
				ClientRevision: revisionError.ClientRevision,
				ServerRevision: revisionError.ServerRevision,
			})
			return
		}
		if errors.Is(err, storage.ErrInstallationConflict) {
			writeJSONErrorResponse(w, http.StatusConflict, protocol.ErrorResponse{
				Error: "another installation is active", Code: "installation_conflict",
			})
			return
		}
		writeJSONError(w, status, "heartbeat rejected")
		return
	}
	if requestHasCapability(r, protocol.NextHeartbeatCapability) {
		response.NextHeartbeatSeconds = int64(nextInterval / time.Second)
		details["next_heartbeat_seconds"] = strconv.FormatInt(response.NextHeartbeatSeconds, 10)
	}
	if !requestHasCapability(r, protocol.CommandAckReceiptCapability) {
		response.AcknowledgedCommands = nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	details["policy_sent"] = strconv.FormatBool(response.Policy != nil)
	details["session_state_sent"] = strconv.FormatBool(response.SessionState != nil)
	details["response_commands"] = strconv.Itoa(len(response.Commands))
	details["acknowledged_events"] = strconv.Itoa(len(response.AcknowledgedEvents))
	a.publishHeartbeatDeviceStatus(deviceID, heartbeatNow)
	for _, command := range response.Commands {
		a.publishDeviceActivity(deviceID, command.ID)
	}
	for _, activityID := range request.CommandAcks {
		a.publishDeviceActivity(deviceID, activityID)
	}
	for _, event := range request.Events {
		if event.Kind == "bonus_added" {
			a.publishDeviceActivity(deviceID, event.UUID)
		}
	}
	_ = json.NewEncoder(w).Encode(response)
}

func writeHeartbeatRateLimit(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	writeJSONErrorResponse(w, http.StatusTooManyRequests, protocol.ErrorResponse{
		Error: "heartbeat rate limited", Code: "rate_limited",
	})
}

func adaptiveHeartbeatInterval(graphicalSessionActive bool) time.Duration {
	if graphicalSessionActive {
		return 5 * time.Second
	}
	return 30 * time.Second
}

func requestHasCapability(r *http.Request, capability string) bool {
	for _, header := range r.Header.Values(protocol.CapabilitiesHeader) {
		for _, advertised := range strings.Split(header, ",") {
			if strings.TrimSpace(advertised) == capability {
				return true
			}
		}
	}
	return false
}

func humanHeartbeatRejection(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "no such table: activity"):
		return "O histórico de atividades do servidor ainda não estava pronto."
	case strings.Contains(message, "FOREIGN KEY constraint failed"):
		return "O servidor encontrou um registro histórico inconsistente."
	case strings.Contains(message, "pending event"), strings.Contains(message, "bonus local"),
		strings.Contains(message, "local bonus"), strings.Contains(message, "bonus local date"):
		return "O servidor não reconheceu uma atividade pendente enviada pelo computador."
	case strings.Contains(message, "command acknowledgement"):
		return "O servidor não reconheceu a confirmação de uma ação anterior."
	case strings.Contains(message, "graphical session"):
		return "O computador enviou uma identificação de sessão que o servidor não reconheceu."
	case strings.Contains(message, "heartbeat counters"), strings.Contains(message, "heartbeat local date"):
		return "O computador enviou informações de tempo que o servidor não conseguiu validar."
	default:
		return "O servidor não conseguiu consolidar a atualização enviada pelo computador."
	}
}

func heartbeatCarriedState(details map[string]string) bool {
	return details["policy_sent"] == "true" || details["session_state_sent"] == "true" ||
		details["response_commands"] != "0" || details["acknowledged_events"] != "0"
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSONErrorResponse(w, status, protocol.ErrorResponse{Error: message})
}

func writeJSONErrorResponse(w http.ResponseWriter, status int, response protocol.ErrorResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
