// Package channelmix 提供固定声道矩阵转换，不执行编解码或改变采样率。
package channelmix

import (
	"fmt"
	"math"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

const PluginID = "musicfs.plugin.decoder.channelmix"

var layouts = map[string][]string{
	"mono":      {"FC"},
	"stereo":    {"FL", "FR"},
	"5.1":       {"FL", "FR", "FC", "LFE", "BL", "BR"},
	"5.1(side)": {"FL", "FR", "FC", "LFE", "SL", "SR"},
	"7.1":       {"FL", "FR", "FC", "LFE", "BL", "BR", "SL", "SR"},
}

type Converter struct {
	input, output audio.Format
	matrix        [][]float64
}

// New 按声道标签生成固定矩阵；上混只保留已有声道，不合成环绕或低音。
func New(input, output audio.Format) (*Converter, error) {
	source, ok := layouts[input.Layout]
	if !ok || input.SampleFormat != "f32le" || input.SampleRate != 48000 || input.Channels != len(source) {
		return nil, fmt.Errorf("UNSUPPORTED: unsupported channel conversion input")
	}
	if output.Validate() != nil || output.Layout == "mono" {
		return nil, fmt.Errorf("UNSUPPORTED: unsupported channel conversion output")
	}
	destination := layouts[output.Layout]
	result := &Converter{input: input, output: output, matrix: make([][]float64, len(destination))}
	for row, name := range destination {
		result.matrix[row] = make([]float64, len(source))
		for column, label := range source {
			result.matrix[row][column] = coefficient(input.Layout, output.Layout, name, label)
		}
		// 以每行绝对系数和归一化，使满幅相关输入也不会因降混而溢出。
		sum := 0.0
		for _, value := range result.matrix[row] {
			sum += math.Abs(value)
		}
		if sum > 1 {
			for column := range source {
				result.matrix[row][column] /= sum
			}
		}
	}
	return result, nil
}

func coefficient(input, output, destination, source string) float64 {
	if input == "mono" && output == "stereo" {
		return 1
	}
	if output == "stereo" {
		if destination == source {
			return 1
		}
		if source == "FC" || destination == "FL" && (source == "BL" || source == "SL") || destination == "FR" && (source == "BR" || source == "SR") {
			return math.Sqrt(0.5)
		}
		return 0
	}
	if output == "5.1" && (destination == "BL" || destination == "BR") {
		sameSide := destination == "BL" && (source == "BL" || source == "SL") || destination == "BR" && (source == "BR" || source == "SR")
		if sameSide {
			if input == "7.1" {
				return math.Sqrt(0.5)
			}
			return 1
		}
	}
	if destination == source {
		return 1
	}
	return 0
}

// Process 转换完整的交错帧；输出帧数和时间位置与输入一致。
func (c *Converter) Process(samples []float32) ([]float32, error) {
	if len(samples)%c.input.Channels != 0 {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: unaligned channel conversion input")
	}
	for _, sample := range samples {
		if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: non-finite channel conversion input")
		}
	}
	frames := len(samples) / c.input.Channels
	result := make([]float32, frames*c.output.Channels)
	for frame := range frames {
		for row, coefficients := range c.matrix {
			value := 0.0
			for column, coefficient := range coefficients {
				value += float64(samples[frame*c.input.Channels+column]) * coefficient
			}
			result[frame*c.output.Channels+row] = float32(value)
		}
	}
	return result, nil
}
