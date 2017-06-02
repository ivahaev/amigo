package amigo

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"testing"
)

var messagePairs = []struct {
	message  string
	expected map[string]string
	err      error
}{
	{
		`Event: EndpointList
ObjectType: endpoint
ObjectName: XXXXX
Transport: transport-udp
Aor: XXX
Auths:
OutboundAuths             :                looong untrimed value
Contacts: XXXX/sip:XXXX
DeviceState: Unavailable
ActiveChannels:
`,
		map[string]string{
			"Event":          "EndpointList",
			"ObjectType":     "endpoint",
			"ObjectName":     "XXXXX",
			"Transport":      "transport-udp",
			"Aor":            "XXX",
			"Auth":           "",
			"OutboundAuths":  "looong untrimed value",
			"Contacts":       "XXXX/sip:XXXX",
			"DeviceState":    "Unavailable",
			"ActiveChannels": "",
		},
		io.EOF,
	},
	{
		`Response: Follows
Privilege: Command
No such command 'core show hi' (type 'core show help core show hi' for other possible commands)
`,
		map[string]string{
			"Response":        "Follows",
			"Privilege":       "Command",
			"CommandResponse": "\nNo such command 'core show hi' (type 'core show help core show hi' for other possible commands)",
		},
		io.EOF,
	},
	{
		`Response: Follows
Privilege: Command
No such command 'core show hi' (type 'core show help core show hi' for other possible commands)
--END COMMAND--
`,
		map[string]string{
			"Response":        "Follows",
			"Privilege":       "Command",
			"CommandResponse": "\nNo such command 'core show hi' (type 'core show help core show hi' for other possible commands)",
		},
		io.EOF,
	},
	{
		`Response: Follows
Privilege: Command
No such command 'core show hi'
(type 'core show help core show hi' for other possible commands)
--END COMMAND--
`,
		map[string]string{
			"Response":        "Follows",
			"Privilege":       "Command",
			"CommandResponse": "\nNo such command 'core show hi'\n(type 'core show help core show hi' for other possible commands)",
		},
		io.EOF,
	},
}

func TestReadMessage(t *testing.T) {
	for i, pair := range messagePairs {
		t.Run("pair "+strconv.Itoa(i), func(t *testing.T) {
			buf := bytes.NewBuffer([]byte(pair.message))
			reader := bufio.NewReader(buf)
			message, err := readMessage(reader)
			if err != pair.err {
				t.Fatalf("readMessage error mismatched. Expected '%v', got '%v'", pair.err, err)
			}
			for k, v := range message {
				if pair.expected[k] != v {
					t.Fatalf("readMessage error. Key '%s', Expected value '%s', got '%s'", k, pair.expected[k], v)
				}
			}
		})
	}
}

func TestToUnicode(t *testing.T) {
	// "Astérix" in ISO-8859-1 bytes
	message := []byte{65, 115, 116, 233, 114, 105, 120}
	message = toUnicode(message)

	expected := "Astérix"
	if string(message) != expected {
		t.Fatalf("unexpected result: want %s got %s", expected, string(message))
	}
}
