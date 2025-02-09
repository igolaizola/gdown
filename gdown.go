package gdown

import (
	"context"
	"log"
	"time"
)

// Run runs the gdown process.
func Run(ctx context.Context) error {
	log.Println("running")
	defer log.Println("finished")
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
	return nil
}
