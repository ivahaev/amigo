package amigo

import (
	"testing"

	"github.com/franela/goblin"
)

func TestAmigo(t *testing.T) {
	g := goblin.Goblin(t)
	g.Describe("$New", func() {
		g.It("should return pointer to a new Amigo struct with username and password filled and default host and port settings", func() {
			a := New("username", "secret")
			g.Assert(a.username).Equal("username")
			g.Assert(a.secret).Equal("secret")
		})
		g.It("should return pointer to a new Amigo struct with username and password filled and provided host and default port settings", func() {
			a := New("username", "secret", "amigo")
			g.Assert(a.username).Equal("username")
			g.Assert(a.secret).Equal("secret")
			g.Assert(a.host).Equal("amigo")
		})
		g.It("should return pointer to a new Amigo struct with username, password, host and port filled", func() {
			a := New("username", "secret", "amigo", "666")
			g.Assert(a.username).Equal("username")
			g.Assert(a.secret).Equal("secret")
			g.Assert(a.host).Equal("amigo")
			g.Assert(a.port).Equal("666")
		})
	})
}
