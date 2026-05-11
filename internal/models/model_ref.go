package models

import (
	"fmt"
	"strings"
)

const DefaultModelEngine = "copilot-sdk"

type ModelRef struct {
	Engine string
	Model  string
}

func ParseModelRef(s, defaultEngine string) (ModelRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ModelRef{}, fmt.Errorf("model is required")
	}

	if defaultEngine == "" {
		defaultEngine = DefaultModelEngine
	}

	engine, model, ok := strings.Cut(s, "/")
	if !ok {
		return ModelRef{Engine: defaultEngine, Model: s}, nil
	}

	engine = strings.TrimSpace(engine)
	model = strings.TrimSpace(model)
	if engine == "" || model == "" {
		return ModelRef{}, fmt.Errorf("invalid model ref %q; expected engine/model", s)
	}

	return ModelRef{Engine: engine, Model: model}, nil
}

func (m ModelRef) String() string {
	if m.Engine == "" {
		return m.Model
	}
	return m.Engine + "/" + m.Model
}
