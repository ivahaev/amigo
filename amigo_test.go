package amigo

import (
	"testing"
)

func TestAmigo(t *testing.T) {
	t.Run("#New", func(t *testing.T) {
		t.Run("should return pointer to a new Amigo struct with username and password filled and default host and port settings", func(t *testing.T) {
			a := New("username", "secret")
			if a.username != "username" {
				t.Fatal("username mismatched")
			}
			if a.secret != "secret" {
				t.Fatal("secret mismatched")
			}
		})
		t.Run("should return pointer to a new Amigo struct with username and password filled and provided host and default port settings", func(t *testing.T) {
			a := New("username", "secret", "amigo")
			if a.username != "username" {
				t.Fatal("username mismatched")
			}
			if a.secret != "secret" {
				t.Fatal("secret mismatched")
			}
			if a.secret != "secret" {
				t.Fatal("secret mismatched")
			}
			if a.host != "amigo" {
				t.Fatal("host mismatched")
			}
		})
		t.Run("should return pointer to a new Amigo struct with username, password, host and port filled", func(t *testing.T) {
			a := New("username", "secret", "amigo", "666")
			if a.username != "username" {
				t.Fatal("username mismatched")
			}
			if a.secret != "secret" {
				t.Fatal("secret mismatched")
			}
			if a.secret != "secret" {
				t.Fatal("secret mismatched")
			}
			if a.host != "amigo" {
				t.Fatal("host mismatched")
			}
			if a.port != "666" {
				t.Fatal("port mismatched")
			}
		})
	})
}
