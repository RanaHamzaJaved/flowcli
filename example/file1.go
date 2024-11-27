package example

import (
	"fmt"

	"github.com/e4coder/flow"
)

func Minus(*flow.ProcessContext, []flow.DefinedInput) error {
	fmt.Println("Minus")

	return nil
}
