package common

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

type BotAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	NewMessage(chatID int64, text string) tgbotapi.MessageConfig
}

type BotAPIWrapper struct {
	api *tgbotapi.BotAPI
}

func NewBotAPIWrapper(api *tgbotapi.BotAPI) *BotAPIWrapper {
	return &BotAPIWrapper{api: api}
}

func (b *BotAPIWrapper) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	return b.api.Send(c)
}

func (b *BotAPIWrapper) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return b.api.GetUpdatesChan(config)
}

func (b *BotAPIWrapper) NewMessage(chatID int64, text string) tgbotapi.MessageConfig {
	return tgbotapi.NewMessage(chatID, text)
}
