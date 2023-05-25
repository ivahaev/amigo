package amigo

import (
	"runtime"
	"testing"
	"time"
)

func TestAmigo(t *testing.T) {
	t.Run("#New", func(t *testing.T) {
		t.Run("should return pointer to a new Amigo struct with username and password filled and default host and port settings", func(t *testing.T) {
			a := New(&Settings{Username: "username", Password: "secret"})
			if a.settings.Username != "username" {
				t.Fatal("username mismatched")
			}
			if a.settings.Password != "secret" {
				t.Fatal("secret mismatched")
			}
		})
		t.Run("should return pointer to a new Amigo struct with username and password filled and provided host and default port settings", func(t *testing.T) {
			a := New(&Settings{Username: "username", Password: "secret", Host: "amigo"})
			if a.settings.Username != "username" {
				t.Fatal("username mismatched")
			}
			if a.settings.Password != "secret" {
				t.Fatal("secret mismatched")
			}
			if a.settings.Host != "amigo" {
				t.Fatal("host mismatched")
			}
		})
	})
}

func TestAmigoClose(t *testing.T) {
	a := New(&Settings{Username: "username", Password: "secret"})
	a.Connect()
	a.Close()
	a.Close()

	time.Sleep(time.Second * 1)
	routines := runtime.NumGoroutine()
	if routines > 2 {
		t.Fatalf("too many go routines, expected 2 got %d", routines)
	}
}
