package cuesheet

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type parser struct {
	sheet        Sheet
	line         int
	fileIndex    int
	trackIndex   int
	lastFrame    int64
	hasStart     bool
	fileHasTrack bool
	paths        map[string]bool
}

// Parse 解析 UTF-8 文本，不读取文件、不创建资源，也不推断末轨时长。
// 相邻音轨按 INDEX 01 划界，下一轨的 INDEX 00 间隙因此归前一轨。
func Parse(text string) (Sheet, error) {
	p := parser{fileIndex: -1, trackIndex: -1, paths: make(map[string]bool)}
	if len(text) > MaxSheetBytes {
		return Sheet{}, p.fail(CodeLimit, "sheet exceeds size limit")
	}
	if !utf8.ValidString(text) {
		return Sheet{}, p.fail(CodeInvalid, "text must be valid UTF-8")
	}
	text = strings.TrimPrefix(text, "\ufeff")
	for i, raw := range strings.Split(text, "\n") {
		p.line = i + 1
		if len(raw) > MaxLineBytes {
			return Sheet{}, p.fail(CodeLimit, "line exceeds size limit")
		}
		line := strings.TrimSuffix(raw, "\r")
		for _, ch := range line {
			if ch < ' ' && ch != '\t' || ch == 0x7f {
				return Sheet{}, p.fail(CodeInvalid, "line contains a control character")
			}
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		command, _, _ := strings.Cut(line, " ")
		if at := strings.IndexByte(command, '\t'); at >= 0 {
			command = command[:at]
		}
		if strings.EqualFold(command, "REM") {
			continue
		}
		parts, ok := tokenize(line)
		if !ok {
			return Sheet{}, p.fail(CodeInvalid, "invalid quoting")
		}
		if err := p.directive(parts); err != nil {
			return Sheet{}, err
		}
	}
	if err := p.finishTrack(); err != nil {
		return Sheet{}, err
	}
	if len(p.sheet.Tracks) == 0 || !p.fileHasTrack {
		return Sheet{}, p.fail(CodeInvalid, "each sheet and file must contain an audio track")
	}
	return p.sheet, nil
}

func (p *parser) fail(code, message string) error {
	return &Error{Code: code, Line: p.line, Message: message}
}

func (p *parser) directive(parts []string) error {
	switch strings.ToUpper(parts[0]) {
	case "FILE":
		return p.file(parts)
	case "TRACK":
		return p.track(parts)
	case "INDEX":
		return p.index(parts)
	case "TITLE", "PERFORMER":
		return p.metadata(parts)
	case "CATALOG", "CDTEXTFILE", "SONGWRITER", "ISRC":
		if len(parts) != 2 {
			return p.fail(CodeInvalid, "metadata requires one value")
		}
		return nil
	case "PREGAP", "POSTGAP", "FLAGS":
		return p.fail(CodeUnsupported, "gap insertion and audio flags are not supported")
	default:
		return p.fail(CodeUnsupported, "directive is not supported")
	}
}

func (p *parser) finishTrack() error {
	if p.trackIndex >= 0 && !p.hasStart {
		return p.fail(CodeInvalid, "each track requires INDEX 01")
	}
	return nil
}

func (p *parser) file(parts []string) error {
	if len(parts) != 3 {
		return p.fail(CodeInvalid, "FILE requires a relative reference and type")
	}
	if err := p.finishTrack(); err != nil {
		return err
	}
	if p.fileIndex >= 0 && !p.fileHasTrack {
		return p.fail(CodeInvalid, "each file must contain an audio track")
	}
	if len(p.sheet.Files) >= MaxFiles {
		return p.fail(CodeLimit, "file count exceeds limit")
	}
	reference, ok := relativeReference(parts[1])
	if !ok {
		return p.fail(CodeInvalid, "FILE reference must stay inside the package")
	}
	if p.paths[reference] {
		return p.fail(CodeInvalid, "duplicate FILE reference")
	}
	kind := strings.ToUpper(parts[2])
	if kind != "WAVE" && kind != "MP3" && kind != "AIFF" {
		return p.fail(CodeUnsupported, "FILE type is not supported")
	}
	p.paths[reference] = true
	p.sheet.Files = append(p.sheet.Files, File{Path: reference, Type: kind})
	p.fileIndex = len(p.sheet.Files) - 1
	p.trackIndex = -1
	p.lastFrame = -1
	p.hasStart = false
	p.fileHasTrack = false
	return nil
}

func (p *parser) track(parts []string) error {
	if len(parts) != 3 || p.fileIndex < 0 {
		return p.fail(CodeInvalid, "TRACK requires a FILE, number and mode")
	}
	if err := p.finishTrack(); err != nil {
		return err
	}
	if len(p.sheet.Tracks) >= MaxTracks {
		return p.fail(CodeLimit, "track count exceeds limit")
	}
	number, err := strconv.Atoi(parts[1])
	if err != nil || !decimal(parts[1]) || number < 1 || number > 99 {
		return p.fail(CodeInvalid, "track number must be between 1 and 99")
	}
	if len(p.sheet.Tracks) > 0 && number <= p.sheet.Tracks[len(p.sheet.Tracks)-1].Number {
		return p.fail(CodeInvalid, "track numbers must be unique and increasing")
	}
	if !strings.EqualFold(parts[2], "AUDIO") {
		return p.fail(CodeUnsupported, "only AUDIO tracks are supported")
	}
	p.sheet.Tracks = append(p.sheet.Tracks, Track{Number: number, FileIndex: p.fileIndex})
	p.trackIndex = len(p.sheet.Tracks) - 1
	p.hasStart = false
	p.fileHasTrack = true
	return nil
}

func (p *parser) index(parts []string) error {
	if len(parts) != 3 || p.trackIndex < 0 {
		return p.fail(CodeInvalid, "INDEX requires a TRACK, number and position")
	}
	if parts[1] != "00" && parts[1] != "01" {
		return p.fail(CodeUnsupported, "only INDEX 00 and INDEX 01 are supported")
	}
	frame, ok := position(parts[2])
	if !ok {
		return p.fail(CodeInvalid, "invalid index position")
	}
	track := &p.sheet.Tracks[p.trackIndex]
	if p.hasStart || parts[1] == "00" && track.Index00Frames != nil {
		return p.fail(CodeInvalid, "indexes must be unique and ordered")
	}
	if frame < p.lastFrame {
		return p.fail(CodeInvalid, "indexes must not move backwards within a file")
	}
	if parts[1] == "00" {
		track.Index00Frames = &frame
	} else {
		if p.trackIndex > 0 {
			previous := &p.sheet.Tracks[p.trackIndex-1]
			if previous.FileIndex == p.fileIndex {
				if frame <= previous.StartFrames {
					return p.fail(CodeInvalid, "track starts must increase within a file")
				}
				previous.EndFrames = &frame
			}
		}
		track.StartFrames = frame
		p.hasStart = true
	}
	p.lastFrame = frame
	return nil
}

func (p *parser) metadata(parts []string) error {
	if len(parts) != 2 {
		return p.fail(CodeInvalid, "metadata requires one value")
	}
	title, performer := &p.sheet.Title, &p.sheet.Performer
	if p.trackIndex >= 0 {
		track := &p.sheet.Tracks[p.trackIndex]
		title, performer = &track.Title, &track.Performer
	} else if len(p.sheet.Tracks) > 0 {
		return p.fail(CodeInvalid, "metadata between FILE and TRACK is ambiguous")
	}
	if strings.EqualFold(parts[0], "TITLE") {
		*title = parts[1]
	} else {
		*performer = parts[1]
	}
	return nil
}
