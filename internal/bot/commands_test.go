package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"sync"
	"testing"

	"YoutubeDownloader/internal/i18n"
	tele "gopkg.in/telebot.v4"
)

func TestCommandMenuKeepsAdminCommandsInAdminChat(t *testing.T) {
	var mu sync.Mutex
	publicLanguages, adminLanguages := map[string]bool{}, map[string]bool{}
	buttons := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch path.Base(r.URL.Path) {
		case "setMyCommands":
			var params tele.CommandParams
			if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
				t.Error(err)
				http.Error(w, "bad JSON", 400)
				return
			}
			if params.Scope == nil {
				t.Error("missing explicit scope")
				http.Error(w, "no scope", 400)
				return
			}
			admin := params.Scope.Type == tele.CommandScopeChat
			if admin {
				if params.Scope.ChatID != 12345 {
					t.Errorf("admin commands exposed to chat %d", params.Scope.ChatID)
				}
				adminLanguages[params.LanguageCode] = true
			} else {
				if params.Scope.Type != tele.CommandScopeDefault {
					t.Errorf("unexpected public scope: %+v", params.Scope)
				}
				publicLanguages[params.LanguageCode] = true
			}
			foundStats := false
			for _, command := range params.Commands {
				if command.Description == "" || command.Description == "menu_"+command.Text {
					t.Errorf("untranslated command: %+v", command)
				}
				if !admin && command.Text != "start" && command.Text != "help" {
					t.Errorf("admin command in public menu: %s", command.Text)
				}
				if command.Text == "stats" {
					foundStats = true
				}
				if command.Text == "admin" {
					t.Error("payment menu should be hidden in free mode")
				}
			}
			if admin && !foundStats {
				t.Error("admin menu is missing stats")
			}
		case "setChatMenuButton":
			var params struct {
				Button tele.MenuButton `json:"menu_button"`
			}
			if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
				t.Error(err)
			}
			if params.Button.Type != tele.MenuButtonCommands {
				t.Errorf("unexpected menu button: %+v", params.Button)
			}
			buttons++
		default:
			t.Errorf("unexpected API request: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	api, err := tele.NewBot(tele.Settings{Token: "test", URL: server.URL, Offline: true, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	manager := i18n.NewManager("ru")
	if err := manager.LoadTranslations("../i18n/translations"); err != nil {
		t.Fatal(err)
	}
	b := &Bot{api: api, config: &BotConfig{AdminID: "12345"}, i18nManager: manager}
	b.setupCommandMenu()
	mu.Lock()
	defer mu.Unlock()
	for _, language := range []string{"", "ru", "en", "es", "fr"} {
		if !publicLanguages[language] || !adminLanguages[language] {
			t.Errorf("missing command registration for language %q", language)
		}
	}
	if buttons != 2 {
		t.Errorf("got %d button settings, want default and admin", buttons)
	}
}
