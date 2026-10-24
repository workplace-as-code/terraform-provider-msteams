package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/provider"
)

var version string = "dev"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "Enable provider debug mode")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/workplace-as-code/msteams",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
