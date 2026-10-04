// Package localmsg holds the human-facing messages shared by the local agent
// API on every operating system. The transport differs between Linux D-Bus and
// the Windows named pipe, but the wording shown to the user must not.
package localmsg

import "strings"

// Synchronization states reported to the local interface. They mirror the
// D-Bus contract of the Linux agent.
const (
	StatusChecking = "checking"
	StatusOnline   = "online"
	StatusOffline  = "offline"
)

// Stable error identifiers returned to the local interface. They replace the
// Linux D-Bus error names, which were prefixed with the bus name.
const (
	ErrorPasswordNotConfigured = "password_not_configured"
	ErrorInvalidPassword       = "invalid_password"
	ErrorRateLimited           = "rate_limited"
	ErrorInvalidRequest        = "invalid_request"
	ErrorFailed                = "failed"
)

// WaitingForFirstResponse is shown while no heartbeat answer has been received
// yet, which is distinct from an answer reporting a failure.
const WaitingForFirstResponse = "Aguardando a primeira resposta do servidor."

// GenericSynchronizationFailure is the safe fallback for an error the agent
// cannot classify, so connection details never reach the interface.
const GenericSynchronizationFailure = "A sincronização falhou. Abra as configurações do Compasso para revisar a comunicação."

// ForBonusFailure is shown when the local bonus could not be stored.
const ForBonusFailure = "Não foi possível adicionar o tempo. Verifique se o Compasso está ativo."

// StatusLabel renders the short connection line used by the interface.
func StatusLabel(state string) string {
	switch state {
	case StatusOnline:
		return "Servidor conectado"
	case StatusChecking:
		return "Aguardando a primeira resposta do servidor…"
	default:
		return "Servidor sem comunicação"
	}
}

// SynchronizationDetail converts a raw synchronization failure into an
// actionable message without disclosing credentials or connection internals.
func SynchronizationDetail(detail string) string {
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
		return GenericSynchronizationFailure
	}
}

// Report combines the live synchronization state with an actionable detail.
func Report(state, rawDetail string) (string, string) {
	switch state {
	case StatusChecking:
		return StatusChecking, WaitingForFirstResponse
	case StatusOnline:
		return StatusOnline, ""
	default:
		return StatusOffline, SynchronizationDetail(rawDetail)
	}
}

// BonusError maps a stable error identifier to the message shown by the
// interface.
func BonusError(code string) string {
	switch code {
	case ErrorPasswordNotConfigured:
		return "Nenhuma senha de administrador foi cadastrada para este dispositivo. Cadastre uma senha no painel do Compasso."
	case ErrorInvalidPassword:
		return "Senha incorreta."
	case ErrorRateLimited:
		return "Muitas tentativas. Aguarde um pouco e tente novamente."
	case ErrorInvalidRequest:
		return "O período escolhido não é válido."
	default:
		return ForBonusFailure
	}
}
