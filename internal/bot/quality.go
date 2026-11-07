package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"YoutubeDownloader/internal/downloader"

	tele "gopkg.in/telebot.v4"
)

type qualityRequest struct {
	url       string
	message   *tele.Message
	userID    int64
	chatID    int64
	createdAt time.Time
	options   map[int]string
}

func validQuality(quality int) bool {
	return quality >= 0 && quality <= 16384
}

func videoCacheKey(url string, quality int) string {
	return fmt.Sprintf("%s|quality=%d|resolution=short-side-v1", url, quality)
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
	options := &tele.SendOptions{}
	if msg.Chat.Type == "group" || msg.Chat.Type == "supergroup" {
		options.ReplyTo = msg
	}
	if !b.downloadManager.AcquireDownloadSlot() {
		_, err := b.api.Send(msg.Chat, b.i18nManager.T(msg.Sender, "too_many_requests"), options)
		return err
	}
	defer b.downloadManager.ReleaseDownloadSlot()
	status, err := b.api.Send(msg.Chat, b.i18nManager.T(msg.Sender, "quality_checking"), options)
	if err != nil {
		return err
	}
	if err := downloader.CheckAndUpdateYTDLp(b.db); err != nil {
		NewLogger("QUALITY").Warning("yt-dlp update: %v", err)
	}
	qualities, err := downloader.AvailableVideoQualities(url)
	prompt := "quality_prompt"
	if err != nil {
		NewLogger("QUALITY").Error("Cannot list formats for %s: %v", url, err)
		if key := downloader.ErrorMessageKey(err); key != "download_failed" {
			_, sendErr := b.api.Edit(status, b.i18nManager.T(msg.Sender, key))
			return sendErr
		}
		// A metadata failure must not prevent trying the original download path.
		qualities = []downloader.QualityOption{{Resolution: 0}}
		prompt = "quality_probe_fallback"
	}
	token := GenerateRequestID()
	request := qualityRequest{url: url, message: msg, userID: msg.Sender.ID, chatID: msg.Chat.ID, createdAt: time.Now(), options: make(map[int]string)}
	markup := &tele.ReplyMarkup{}
	for i, quality := range qualities {
		label := fmt.Sprintf("%dp", quality.Resolution)
		if quality.Resolution == 0 {
			label = b.i18nManager.T(msg.Sender, "quality_original")
		} else if quality.Convert {
			label = b.i18nManager.T(msg.Sender, "quality_convert", quality.Resolution)
		}
		request.options[quality.Resolution] = label
		if i%2 == 0 {
			markup.InlineKeyboard = append(markup.InlineKeyboard, []tele.InlineButton{})
		}
		row := len(markup.InlineKeyboard) - 1
		markup.InlineKeyboard[row] = append(markup.InlineKeyboard[row], tele.InlineButton{Text: label, Data: fmt.Sprintf("quality|%s|%d", token, quality.Resolution)})
	}
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

	if _, err := b.api.Edit(status, b.i18nManager.T(msg.Sender, prompt), markup); err != nil {
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
	label, offered := request.options[quality]
	if ok && offered && request.userID == c.Sender().ID && request.chatID == c.Chat().ID && time.Since(request.createdAt) <= 30*time.Minute {
		delete(b.pendingQuality, parts[1])
	} else {
		ok = false
	}
	b.qualityMutex.Unlock()
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Выбор недоступен. Отправьте ссылку ещё раз."})
	}
	_ = c.Respond()
	if _, err := b.api.Edit(c.Callback().Message, b.i18nManager.T(c.Sender(), "quality_selected", label), &tele.ReplyMarkup{}); err != nil {
		NewLogger("QUALITY").Warning("Не удалось убрать кнопки выбора качества: %v", err)
	}
	isAdmin := b.config.AdminID != "" && b.config.AdminID == toStr(c.Sender().ID)
	return b.handleSelectedURL(c, request.message, request.url, isAdmin, quality)
}
