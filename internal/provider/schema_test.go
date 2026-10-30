package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// TestProviderSchema loads the provider schema the way Terraform does. The
// framework rejects two resources or two data sources with the same name and
// any invalid schema, which would otherwise only show up on a real run.
func TestProviderSchema(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("%s: %s", d.Summary, d.Detail)
		}
	}
	if len(resp.DataSourceSchemas) == 0 || len(resp.ResourceSchemas) == 0 {
		t.Fatalf("%d data sources, %d resources", len(resp.DataSourceSchemas), len(resp.ResourceSchemas))
	}
}
