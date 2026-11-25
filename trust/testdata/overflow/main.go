// Command overflow is a program built as programs are, not a test binary, that walks the folder
// it is given with Who{} and with Who.Overflow set, which no program may set, and prints a line for
// each walk: whether it was refused, and whether the refusal matches ErrOverflow and
// errors.ErrUnsupported. A last line says whether Account, and ReadOwned of this
// program's own file, were refused with ErrOverflow, as they are for the overflow account.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/birdtable/go-files/trust"
)

func main() {
	for _, who := range []trust.Who{{}, {Overflow: true}} {
		_, err := trust.Path(os.Args[1], trust.Folder, who)
		fmt.Printf("overflow=%t refused=%t ErrOverflow=%t ErrUnsupported=%t: %v\n",
			who.Overflow, err != nil, errors.Is(err, trust.ErrOverflow), errors.Is(err, errors.ErrUnsupported), err)
	}
	me, account := trust.Account()
	_, read := trust.ReadOwned(os.Args[0], me, false, 1<<30)
	fmt.Printf("Account ErrOverflow=%t ReadOwned ErrOverflow=%t\n", errors.Is(account, trust.ErrOverflow), errors.Is(read, trust.ErrOverflow))
}
