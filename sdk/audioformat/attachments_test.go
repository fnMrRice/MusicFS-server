package audioformat_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

func TestChainRetainsAttachmentsAndOwnersFromAllLayers(t *testing.T) {
	closed := 0
	stage := func(name, next string, more bool) audioformat.Decoder {
		return chainDecoder(name, 1, name, func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
			return audioformat.Output{
				Input: chainInput(next), NeedsDecoding: more,
				Attachments: []audioformat.Attachment{{Kind: "artwork", Size: int64(len(name)), Reader: strings.NewReader(name)}},
				Close:       func() error { closed++; return nil },
			}, nil
		})
	}
	chain, err := audioformat.NewChain(stage("outer", "inner", true), stage("inner", "audio", false))
	if err != nil {
		t.Fatal(err)
	}
	result, err := chain.Decode(t.Context(), audioformat.Request{}, chainInput("outer"))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	if len(result.Attachments) != 2 || closed != 0 {
		t.Fatal("container attachment or owning layer was lost")
	}
	for index, expected := range []string{"outer", "inner"} {
		item := result.Attachments[index]
		body, err := io.ReadAll(io.NewSectionReader(item.Reader, 0, item.Size))
		if err != nil || string(body) != expected || item.Kind != "artwork" {
			t.Fatal("attachment changed across decode layers", err)
		}
	}
	if err := result.Close(); err != nil || closed != 2 {
		t.Fatal("attachment owners were not closed", err)
	}
}

func TestChainRejectsInvalidAttachmentsAndClosesOwningLayer(t *testing.T) {
	valid := audioformat.Attachment{Kind: "artwork", Reader: strings.NewReader("image"), Size: 5}
	for _, mutate := range []func(*audioformat.Attachment){
		func(a *audioformat.Attachment) { a.Kind = "audio" },
		func(a *audioformat.Attachment) { a.Reader = nil },
		func(a *audioformat.Attachment) { a.Size = 0 },
		func(a *audioformat.Attachment) { a.Size = (8 << 40) + 1 },
	} {
		closed := false
		item := valid
		mutate(&item)
		chain, err := audioformat.NewChain(chainDecoder("source", 1, "source", func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
			return audioformat.Output{Input: chainInput("audio"), Attachments: []audioformat.Attachment{item}, Close: func() error { closed = true; return nil }}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		if result, err := chain.Decode(t.Context(), audioformat.Request{}, chainInput("source")); err == nil || result != nil || !closed {
			t.Fatal("invalid attachment accepted or leaked its owner", err)
		}
	}
}

func TestChainAttachmentBudgetCoversAllLayers(t *testing.T) {
	closed, calls := 0, 0
	chain, err := audioformat.NewChain(chainDecoder("source", 1, "", func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
		calls++
		items := make([]audioformat.Attachment, 20)
		for i := range items {
			items[i] = audioformat.Attachment{Kind: "artwork", Reader: strings.NewReader("image"), Size: 5}
		}
		return audioformat.Output{Input: chainInput(fmt.Sprintf("layer-%d", calls)), Attachments: items, NeedsDecoding: true, Close: func() error { closed++; return nil }}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := chain.Decode(t.Context(), audioformat.Request{}, chainInput("source")); err == nil || result != nil || calls != 2 || closed != 2 {
		t.Fatal("attachment budget did not bound the complete chain", err, calls, closed)
	}
}
