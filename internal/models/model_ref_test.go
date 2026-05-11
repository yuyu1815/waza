package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseModelRef(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		defaultEngine string
		want          ModelRef
		wantErr       bool
	}{
		{
			name:          "prefixless uses provided default",
			input:         "gpt-4o",
			defaultEngine: "mock",
			want:          ModelRef{Engine: "mock", Model: "gpt-4o"},
		},
		{
			name:  "prefixless uses copilot default",
			input: "gpt-4o",
			want:  ModelRef{Engine: DefaultModelEngine, Model: "gpt-4o"},
		},
		{
			name:  "first slash separates engine only",
			input: "claude-sdk/vendor/model",
			want:  ModelRef{Engine: "claude-sdk", Model: "vendor/model"},
		},
		{
			name:  "double slash keeps slash in model",
			input: "claude-sdk//sonnet",
			want:  ModelRef{Engine: "claude-sdk", Model: "/sonnet"},
		},
		{
			name:    "empty engine",
			input:   "/sonnet",
			wantErr: true,
		},
		{
			name:    "empty model",
			input:   "claude-sdk/",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   " ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModelRef(tt.input, tt.defaultEngine)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModelRefString(t *testing.T) {
	assert.Equal(t, "claude-sdk/sonnet", ModelRef{Engine: "claude-sdk", Model: "sonnet"}.String())
	assert.Equal(t, "sonnet", ModelRef{Model: "sonnet"}.String())
}
