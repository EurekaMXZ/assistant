package postgres

import (
	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	assistantmail "github.com/EurekaMXZ/assistant/internal/mail"
	"github.com/EurekaMXZ/assistant/internal/profile"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

var (
	_ tool.ConversationTitleUpdater  = (*ConversationRepository)(nil)
	_ tool.ConversationSandboxStore  = (*ConversationSandboxRepository)(nil)
	_ assistantauth.UserStore        = (*UserRepository)(nil)
	_ assistantauth.ActionTokenStore = (*ActionTokenRepository)(nil)
	_ assistantmail.SettingsStore    = (*SMTPSettingsRepository)(nil)
	_ profile.Repository             = (*ProfileRepository)(nil)
)
