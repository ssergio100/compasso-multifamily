// Package localcontrol contains behavior shared by the local Linux and
// Windows agent interfaces.
package localcontrol

import "strings"

type SynchronizationSource interface {
	SynchronizationStatus() (checked, online bool)
}

type synchronizationReportSource interface {
	SynchronizationReport() (checked, online bool, detail string)
}

// SynchronizationReport returns the stable local-UI state and a sanitized,
// actionable explanation. Raw transport errors must not cross the local API.
func SynchronizationReport(source SynchronizationSource) (state, detail string) {
	checked, online := source.SynchronizationStatus()
	if reportSource, ok := source.(synchronizationReportSource); ok {
		checked, online, detail = reportSource.SynchronizationReport()
	}
	if !checked {
		return "checking", "Aguardando a primeira resposta do servidor."
	}
	if online {
		return "online", ""
	}
	return "offline", HumanSynchronizationDetail(detail)
}

func HumanSynchronizationDetail(detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "http 400"):
		return "O servidor recusou a sincronização (erro 400). O agente e o servidor podem estar em versões incompatíveis."
	case strings.Contains(lower, "http 401"), strings.Contains(lower, "http 403"):
		return "O servidor recusou a identificação deste computador. Revise o dispositivo e o token nas configurações."
	case strings.Contains(lower, "local revision"), strings.Contains(lower, "http 409"):
		return "O estado deste computador não corresponde ao cadastro atual do servidor. Revise a configuração do dispositivo."
	case strings.Contains(lower, "update the compasso agent"), strings.Contains(lower, "http 426"):
		return "Este agente precisa ser atualizado para continuar a comunicação com o servidor."
	case strings.Contains(lower, "http 502"), strings.Contains(lower, "http 503"), strings.Contains(lower, "http 504"):
		return "O servidor está temporariamente indisponível. O agente continuará tentando automaticamente."
	case strings.Contains(lower, "lookup "), strings.Contains(lower, "no such host"), strings.Contains(lower, "server misbehaving"):
		return "Não foi possível localizar o servidor na rede. Verifique a conexão e o endereço configurado."
	case strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "timeout"):
		return "O servidor demorou demais para responder. O agente continuará tentando automaticamente."
	default:
		return "A sincronização falhou. Abra as configurações do Compasso para revisar a comunicação."
	}
}
