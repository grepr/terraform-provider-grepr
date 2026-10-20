package integration

import (
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
)

type PipeModel struct {
	Common
}

var NewPipeResource = newResource(Kind[PipeModel, generated.GreprPipe]{
	Type:         "grepr-pipe",
	Name:         "pipe",
	Description:  "Manages a Grepr pipe integration, which carries log events from one pipeline to another: pipelines write into it with a `grepr-pipe-log-sink` and read from it with a `grepr-pipe-log-source`.",
	ToPayload:    func(PipeModel, *generated.GreprPipe) {},
	FromPayload:  func(generated.GreprPipe, *PipeModel) {},
	WriteSecrets: func(*secretWriter, PipeModel, *PipeModel) {},
})
