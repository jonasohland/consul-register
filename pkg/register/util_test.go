package register_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/jonasohland/consul-register/pkg/register"
)

func TestJi(t *testing.T) {

	fmt.Println(register.Jitter(5*time.Second, 2*time.Second))

}
