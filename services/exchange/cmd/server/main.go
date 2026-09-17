package main

import (
	"log/slog"
	"os"

	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/app"
)

func main() {
	ctx, stop := runtime.Context("exchange")
	defer stop()
	if err := app.Run(ctx); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
