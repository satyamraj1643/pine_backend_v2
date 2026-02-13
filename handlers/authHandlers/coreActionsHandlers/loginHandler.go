package coreactions

import (

"net/http"
"context"
"fmt"
"log"
""

)

func HandleLogin(w http.ResponseWriter, r *http.Request) {
     
	// this is a no auth route, hence no need to verify the OPT, proceed with parsing the input structure.

	fmt.Println("in login")

	if r.Method != ht

}