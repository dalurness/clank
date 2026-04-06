package main

import (
	"fmt"

	clank "github.com/dalurness/clank"
)

func cmdSpec() int {
	fmt.Print(clank.Spec)
	return 0
}
