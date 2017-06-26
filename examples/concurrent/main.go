// Created by ivahaev at 2017-06-25
//
// This example demonstrates how you can handle events in goroutines.
// Only three events handled in this example — Dial.Begin, Dial.End and Hangup.
// On each call we create new channel and start new goroutine with channel reader.
// On Hangup event we close channel and it kills created goroutine.
// Also it demonstrates how we can expose AMI actions (func setVar)

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ivahaev/amigo"
)

var a *amigo.Amigo

func setVar(channel, variable, value string) error {
	_, err := a.Action(map[string]string{"Action": "SetVar", "Channel": channel, "Variable": variable, "Value": value})

	return err
}

func dialBeginHandler(e map[string]string) {
	fmt.Println("DialBegin event: ", e)
	// emulating long handling
	time.Sleep(5 * time.Second)
}
func dialEndHandler(e map[string]string) {
	fmt.Println("DialEnd event: ", e)
	// emulating very long handling
	time.Sleep(10 * time.Second)
}
func hangupHandler(e map[string]string) {
	fmt.Println("Hangup event: ", e)
}

func eventsProcessor(ch <-chan map[string]string) {
	for e := range ch {
		switch e["Event"] {
		case "Dial":
			switch e["SubEvent"] {
			case "Begin":
				dialBeginHandler(e)
			case "End":
				dialEndHandler(e)
			default:
				fmt.Printf("Unknown Dial SubEvent: %s\n", e["SubEvent"])
			}
		case "DialBegin":
			dialBeginHandler(e)
		case "DialEnd":
			dialEndHandler(e)
		case "Hangup":
			hangupHandler(e)
		}
	}
}

func main() {
	settings := &amigo.Settings{Host: "127.0.0.1", Port: "5038", Username: "admin", Password: "amp111"}
	if e := os.Getenv("AMIGO_HOST"); len(e) > 0 {
		settings.Host = e
	}
	if e := os.Getenv("AMIGO_PORT"); len(e) > 0 {
		settings.Port = e
	}
	if e := os.Getenv("AMIGO_USERNAME"); len(e) > 0 {
		settings.Username = e
	}
	if e := os.Getenv("AMIGO_PASSWORD"); len(e) > 0 {
		settings.Password = e
	}

	a = amigo.New(settings)
	a.On("connect", func(message string) {
		fmt.Println("Connected", message)
	})
	a.On("error", func(message string) {
		fmt.Println("Connection error:", message)
	})

	a.Connect()

	chans := map[string]chan map[string]string{}
	c := make(chan map[string]string, 100)
	a.SetEventChannel(c)

	for e := range c {
		uniqueID := e["UniqueID"]
		if len(uniqueID) == 0 {
			uniqueID = e["Uniqueid"]
		}

		switch e["Event"] {
		case "Dial":
			{
				if ch, ok := chans[uniqueID]; ok {
					ch <- e
					continue
				}

				ch := make(chan map[string]string, 3) // capacity is 3 because only 3 events will handled
				ch <- e
				chans[uniqueID] = ch
				go eventsProcessor(ch)
			}
		case "DialBegin": // asterisk 13
			{
				ch := make(chan map[string]string, 3)
				ch <- e
				chans[uniqueID] = ch
				go eventsProcessor(ch)
			}
		case "DialEnd": // asterisk 13
			if ch, ok := chans[uniqueID]; ok {
				ch <- e
				continue
			}

			fmt.Printf("Unknown UniqueID on DialEnd event: %s\n", uniqueID)
		case "Hangup":
			if ch, ok := chans[uniqueID]; ok {
				ch <- e
				close(ch)
				delete(chans, uniqueID)
				continue
			}

			fmt.Printf("Unknown UniqueID on Hangup event: %s\n", uniqueID)
		}
	}
}
