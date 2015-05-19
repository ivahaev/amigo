# amigo
Asterisk AMI connector in golang

Usage is pretty simple.

To install:
`go get github.com/ivahaev/amigo`

Then import module to your project:
```go
import "github.com/ivahaev/amigo"
```

Then use:
```go
import (
    "github.com/ivahaev/amigo"
    "github.com/ivahaev/go-logger"
)


func main() {
    // Creating channel to receiving events
    c := make(chan map[string]string, 100)
    
    // Connects with Asterisk
    go amigo.Connect(c, "username", "secret", "host", "port")
    
    // Gorutine will handle all Asterisk Events
    go func() {
        for {
            var e = <-c
            logger.Debug(e)
        }
    }()
    
    // Check if connected with Asterisk, will send Action "QueueSummary"
    if amigo.Connected() {
        result, err := amigo.Action(map[string]string{"Action": "QueueSummary", "ActionID": "Init"})
        logger.Debug(result, err)
    }
}
```
