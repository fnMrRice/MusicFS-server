package audio

import "fmt"

// Output 声明节点输出格式，以及该输出是否允许继续进入其他 DSP 节点。
type Output struct {
	Format         Format `json:"format"`
	CanContinueDSP bool   `json:"canContinueDSP"`
}

// IOConfigurer 在配置时协商输入和输出；旧 Node 仍按同格式 PCM 节点兼容。
type IOConfigurer interface {
	ConfigureIO(input Format) (Output, error)
}

// IODescriber 无需启动处理或修改节点状态即可检查输入和输出约束。
type IODescriber interface {
	DescribeIO(input Format) (Output, error)
}

// PCMOutput 返回可继续处理的同格式 PCM 声明。
func PCMOutput(input Format) (Output, error) {
	if err := input.Validate(); err != nil {
		return Output{}, err
	}
	return Output{Format: input, CanContinueDSP: true}, nil
}

// DescribeIO 读取显式声明；兼容旧节点时固定保持输入格式。
func DescribeIO(node Node, input Format) (Output, error) {
	if node == nil {
		return Output{}, fmt.Errorf("invalid DSP node")
	}
	if err := input.Validate(); err != nil {
		return Output{}, err
	}
	if descriptor, ok := node.(IODescriber); ok {
		output, err := descriptor.DescribeIO(input)
		if err != nil {
			return Output{}, err
		}
		return validateOutput(input, output)
	}
	if _, explicit := node.(IOConfigurer); explicit {
		return Output{}, fmt.Errorf("UNSUPPORTED: DSP node has no read-only IO declaration")
	}
	return PCMOutput(input)
}

func configureIO(node Node, input Format) (Output, error) {
	if configurer, ok := node.(IOConfigurer); ok {
		output, err := configurer.ConfigureIO(input)
		if err != nil {
			return Output{}, err
		}
		return validateOutput(input, output)
	}
	if err := node.Configure(input); err != nil {
		return Output{}, err
	}
	return PCMOutput(input)
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
		output, err = DescribeIO(node, output.Format)
		if err != nil {
			return Output{}, fmt.Errorf("DSP node %d: %w", index, err)
		}
	}
	return output, nil
}

// Output 仅在 Configure 或 ConfigureIO 成功后返回协商结果。
func (c *Chain) Output() Output { return c.output }
