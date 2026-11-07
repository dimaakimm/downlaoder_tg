package bot

import (
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v4"
)

func (b *Bot) menuCommands(language string, admin bool) []tele.Command {
	user := &tele.User{LanguageCode: language}
	commands := []tele.Command{
		{Text: "start", Description: b.i18nManager.T(user, "menu_start")},
		{Text: "help", Description: b.i18nManager.T(user, "menu_help")},
	}
	if admin {
		for _, name := range []string{"stats", "weeklystats", "userstats", "active_downloads", "cache_stats", "bot_info", "api_info"} {
			commands = append(commands, tele.Command{Text: name, Description: b.i18nManager.T(user, "menu_"+name)})
		}
		if b.config.PaymentEnabled {
			commands = append(commands, tele.Command{Text: "admin", Description: b.i18nManager.T(user, "menu_admin")})
		}
	}
	return commands
}

// Chat-scoped commands affect visibility only; handlers still check ADMIN_ID.
func (b *Bot) setupCommandMenu() {
	logger := NewLogger("COMMAND_MENU")
	adminID, err := strconv.ParseInt(strings.TrimSpace(b.config.AdminID), 10, 64)
	hasAdmin := err == nil && adminID > 0
	if b.config.AdminID != "" && !hasAdmin {
		logger.Warning("ADMIN_ID должен быть положительным числовым Telegram ID")
	}
	for _, language := range []string{"", "ru", "en", "es", "fr"} {
		if err := b.api.SetCommands(b.menuCommands(language, false), language, tele.CommandScope{Type: tele.CommandScopeDefault}); err != nil {
			logger.Warning("Не удалось настроить меню пользователей (язык %q): %v", language, err)
		}
		if hasAdmin {
			scope := tele.CommandScope{Type: tele.CommandScopeChat, ChatID: adminID}
			if err := b.api.SetCommands(b.menuCommands(language, true), language, scope); err != nil {
				logger.Warning("Не удалось настроить меню администратора (язык %q): %v", language, err)
			}
		}
	}
	if err := b.api.SetMenuButton(nil, tele.MenuButtonCommands); err != nil {
		logger.Warning("Не удалось включить кнопку меню: %v", err)
	}
	if hasAdmin {
		if err := b.api.SetMenuButton(&tele.User{ID: adminID}, tele.MenuButtonCommands); err != nil {
			logger.Warning("Не удалось включить кнопку меню администратора: %v", err)
		}
	}
}
