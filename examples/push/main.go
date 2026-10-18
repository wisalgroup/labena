// Run with PUSH_EXPO_ACCESS_TOKEN (if enabled in Expo) and PUSH_REGISTRATION.
// Sends a real test alert to the supplied registration. No database or queue is used.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/wisalgroup/labena/pkg/push"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sender, err := push.NewExpo(push.ExpoConfig{AccessToken: os.Getenv("PUSH_EXPO_ACCESS_TOKEN")})
	if err != nil {
		log.Fatal(err)
	}
	if id := os.Getenv("PUSH_TICKET_ID"); id != "" {
		receipt, err := sender.Receipt(ctx, id)
		if err != nil {
			log.Fatal(err)
		}
		if !receipt.Found {
			log.Print("Receipt pending; retry lookup later")
			return
		}
		if receipt.Error != nil {
			log.Fatal(receipt.Error)
		}
		log.Print("Expo confirmed platform handoff")
		return
	}
	expires := time.Now().Add(time.Hour)
	ticket, err := sender.Send(ctx, os.Getenv("PUSH_REGISTRATION"), push.Message{Title: "Push is connected", Body: "Your application can now receive alerts.", Data: map[string]string{"example": "setup"}, ExpiresAt: &expires})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Expo accepted ticket %s; check its receipt after 15 minutes", ticket.ID)
}
