package main

import (
	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"example.com/terraform-k8s-e2e-reference/services/settlement/internal/app"
	"log/slog"
	"os"
)

func main() {
	ctx, stop := runtime.Context("settlement")
	defer stop()
	if err := app.Run(ctx); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
