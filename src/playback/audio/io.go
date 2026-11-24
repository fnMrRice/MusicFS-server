package audio

import "fmt"

// Output 声明节点输出格式，以及该输出是否允许继续进入其他 DSP 节点。
type Output struct {
	Format         Format `json:"format"`
	CanContinueDSP bool   `json:"canContinueDSP"`
}

// PCMOutput 返回可继续处理的同格式 PCM 声明。
func PCMOutput(input Format) (Output, error) {
	if err := input.Validate(); err != nil {
		return Output{}, err
	}
	return Output{Format: input, CanContinueDSP: true}, nil
}

// DescribeIO 校验节点的只读格式声明。
func DescribeIO(node Node, input Format) (Output, error) {
	if node == nil {
		return Output{}, fmt.Errorf("invalid DSP node")
	}
	if err := input.Validate(); err != nil {
		return Output{}, err
	}
	output, err := node.DescribeIO(input)
	if err != nil {
		return Output{}, err
	}
	return validateOutput(input, output)
}

func validateOutput(input Format, output Output) (Output, error) {
	if err := output.Format.Validate(); err != nil {
		return Output{}, fmt.Errorf("UNSUPPORTED: invalid DSP output format: %w", err)
	}
	if output.Format.SampleRate != input.SampleRate {
		return Output{}, fmt.Errorf("UNSUPPORTED: DSP cannot change the sample timeline")
	}
	return output, nil
}

// NegotiateChain 仅通过只读声明校验节点顺序与格式；工厂应当可安全用于预览。
func NegotiateChain(registry *Registry, specs []NodeSpec, input Format) (Output, error) {
	chain, err := NewChain(registry, specs)
	if err != nil {
		return Output{}, err
	}
	defer chain.Close()
	return chain.DescribeIO(input)
}

func (c *Chain) DescribeIO(input Format) (Output, error) {
	output, err := PCMOutput(input)
	if err != nil {
		return Output{}, err
	}
	for index, node := range c.nodes {
		if !output.CanContinueDSP {
			return Output{}, fmt.Errorf("UNSUPPORTED: DSP node %d follows a terminal output", index)
		}
		output, err = node.DescribeIO(output.Format)
		if err != nil {
			return Output{}, fmt.Errorf("DSP node %d: %w", index, err)
		}
	}
	return output, nil
}

// Output 在 ConfigureIO 成功后返回协商结果。
func (c *Chain) Output() Output { return c.output }
