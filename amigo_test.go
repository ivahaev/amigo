package amigo

import (
	"testing"
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
