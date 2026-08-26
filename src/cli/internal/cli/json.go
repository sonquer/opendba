package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

func writeJSONValue(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write the result: %w", err)
	}
	return nil
}
