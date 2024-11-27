package example

import (
	"fmt"

	"github.com/e4coder/flow"
)

func Multiply(*flow.ProcessContext, []flow.DefinedInput) error {
	fmt.Println("Multiple")
	return nil
}
