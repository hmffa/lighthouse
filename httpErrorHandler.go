package lighthouse

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	log "github.com/sirupsen/logrus"
)

func handleError(ctx *fiber.Ctx, err error) error {
	// Status code defaults to 500
	code := fiber.StatusInternalServerError
	msg := err.Error()

	var e *fiber.Error
	if errors.As(err, &e) {
		code = e.Code
		msg = e.Error()
	} else {
		// Log the original error internally so it's not lost
		log.WithError(err).Error("Internal Server Error in HTTP handler")
		// and use a generic message to prevent information disclosure.
		msg = "Internal Server Error"
	}
	return ctx.Status(code).JSON(fiber.Map{"error": msg})
}
