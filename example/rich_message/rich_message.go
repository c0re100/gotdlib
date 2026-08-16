package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	tdlib "github.com/c0re100/gotdlib/client"
	"github.com/c0re100/gotdlib/helper"
)

func GetTdParameters() *tdlib.SetTdlibParametersRequest {
	return &tdlib.SetTdlibParametersRequest{
		UseTestDc:           false,
		DatabaseDirectory:   "./tdlib-db",
		FilesDirectory:      "./tdlib-files",
		UseFileDatabase:     true,
		UseChatInfoDatabase: true,
		UseMessageDatabase:  true,
		UseSecretChats:      false,
		ApiId:               132712,
		ApiHash:             "e82c07ad653399a37baca8d1e498e472",
		SystemLanguageCode:  "en",
		DeviceModel:         "HuskyNG",
		SystemVersion:       "3.0",
		ApplicationVersion:  "3.0",
	}
}

func main() {
	tdlib.SetLogLevel(0)
	tdlib.SetFilePath("./errors.txt")

	botToken := "your_bot_token"
	authorizer := tdlib.BotAuthorizer(botToken)

	authorizer.TdlibParameters <- GetTdParameters()

	client, err := tdlib.NewClient(authorizer)
	if err != nil {
		log.Fatalf("NewClient error: %s", err)
	}

	// Handle SIGINT
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGINT)
	signal.Notify(ch, os.Interrupt, syscall.SIGKILL)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	signal.Notify(ch, os.Interrupt, syscall.SIGQUIT)
	signal.Notify(ch, os.Interrupt, syscall.SIGSEGV)
	go func() {
		<-ch
		client.Close()
	}()

	me, err := client.GetMe()
	if err != nil {
		log.Fatalf("GetMe error: %s", err)
	}

	log.Printf("%v connected", me.Usernames)

	listener := client.AddEventReceiver(&tdlib.UpdateNewMessage{}, 1000)

	defer listener.Close()
	for update := range listener.Updates {
		updateMsg := update.(*tdlib.UpdateNewMessage)

		switch updateMsg.Message.Content.MessageContentType() {
		case tdlib.TypeMessageRichMessage:
			richMsg := updateMsg.Message.Content.(*tdlib.MessageRichMessage).Message
			formatted, gErr := helper.GetTextAndEntitiesFromRichMessage(richMsg)
			if gErr != nil {
				log.Println(gErr)
				return
			}
			msgText := formatted.Text
			fmt.Println(msgText)
		}
	}
}
