package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"
)

type qualityRequest struct {
	url       string
	message   *tele.Message
	userID    int64
	chatID    int64
	createdAt time.Time
}

func validQuality(quality int) bool {
	return quality == 1080 || quality == 720 || quality == 480
}

func videoCacheKey(url string, quality int) string {
	return fmt.Sprintf("%s|quality=%d", url, quality)
}

func videoPayload(url string, quality int) string {
	return fmt.Sprintf("video|%d|%s", quality, url)
}

// Legacy invoices have no quality and retain the former highest-quality default.
func parseVideoPayload(payload string) (int, string) {
	parts := strings.SplitN(payload, "|", 2)
	if len(parts) == 2 {
		if quality, err := strconv.Atoi(parts[0]); err == nil && validQuality(quality) {
			return quality, parts[1]
		}
	}
	return 1080, payload
}

func (b *Bot) askVideoQuality(msg *tele.Message, url string) error {
	token := GenerateRequestID()
	request := qualityRequest{url: url, message: msg, userID: msg.Sender.ID, chatID: msg.Chat.ID, createdAt: time.Now()}
	b.qualityMutex.Lock()
	if b.pendingQuality == nil {
		b.pendingQuality = make(map[string]qualityRequest)
	}
	for key, pending := range b.pendingQuality {
		if time.Since(pending.createdAt) > 30*time.Minute {
			delete(b.pendingQuality, key)
		}
	}
	b.pendingQuality[token] = request
	b.qualityMutex.Unlock()

	markup := &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{
		{Text: "1080p", Data: "quality|" + token + "|1080"},
		{Text: "720p", Data: "quality|" + token + "|720"},
		{Text: "480p", Data: "quality|" + token + "|480"},
	}}}
	options := &tele.SendOptions{ReplyMarkup: markup}
	if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
		options.ReplyTo = msg
	}
	if _, err := b.api.Send(msg.Chat, "Выберите качество видео:", options); err != nil {
		b.qualityMutex.Lock()
		delete(b.pendingQuality, token)
		b.qualityMutex.Unlock()
		return err
	}
	return nil
}

func (b *Bot) handleQualityCallback(c tele.Context, data string) error {
	parts := strings.Split(data, "|")
	if len(parts) != 3 {
		return c.Respond(&tele.CallbackResponse{Text: "Неверный выбор качества"})
	}
	quality, err := strconv.Atoi(parts[2])
	if err != nil || !validQuality(quality) {
		return c.Respond(&tele.CallbackResponse{Text: "Неверный выбор качества"})
	}
	b.qualityMutex.Lock()
	request, ok := b.pendingQuality[parts[1]]
	if ok && request.userID == c.Sender().ID && request.chatID == c.Chat().ID && time.Since(request.createdAt) <= 30*time.Minute {
		delete(b.pendingQuality, parts[1])
	} else {
		ok = false
	}
	b.qualityMutex.Unlock()
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Выбор недоступен. Отправьте ссылку ещё раз."})
	}
	_ = c.Respond()
	if _, err := b.api.Edit(c.Callback().Message, fmt.Sprintf("Выбрано качество: %dp", quality), &tele.ReplyMarkup{}); err != nil {
		NewLogger("QUALITY").Warning("Не удалось убрать кнопки выбора качества: %v", err)
	}
	isAdmin := b.config.AdminID != "" && b.config.AdminID == toStr(c.Sender().ID)
	return b.handleSelectedURL(c, request.message, request.url, isAdmin, quality)
}
