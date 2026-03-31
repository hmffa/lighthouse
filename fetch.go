package lighthouse

import (
	"github.com/go-oidfed/lib/oidfedconst"
	"github.com/gofiber/fiber/v2"
	log "github.com/sirupsen/logrus"

	"github.com/go-oidfed/lib"

	"github.com/go-oidfed/lighthouse/storage/model"
)

// AddFetchEndpoint adds a fetch endpoint
func (fed *LightHouse) AddFetchEndpoint(endpoint EndpointConf, store model.SubordinateStorageBackend) {
	fed.fedMetadata.FederationFetchEndpoint = endpoint.ValidateURL(fed.FederationEntity.EntityID())
	if endpoint.Path == "" {
		return
	}
	fed.server.Get(
		endpoint.Path, func(ctx *fiber.Ctx) error {
			sub := ctx.Query("sub")
			if sub == "" {
				ctx.Status(fiber.StatusBadRequest)
				return ctx.JSON(oidfed.ErrorInvalidRequest("required parameter 'sub' not given"))
			}
			info, err := store.Get(sub)
			if err != nil {
				log.WithError(err).Error("failed to get from store")
				ctx.Status(fiber.StatusInternalServerError)
				return ctx.JSON(oidfed.ErrorServerError("internal server error"))
			}
			if info == nil {
				ctx.Status(fiber.StatusNotFound)
				return ctx.JSON(oidfed.ErrorNotFound("the requested entity identifier is not found"))
			}
			payload := fed.CreateSubordinateStatement(info)
			jwt, err := fed.SignEntityStatement(payload)
			if err != nil {
				log.WithError(err).Error("failed to sign entity statement")
				ctx.Status(fiber.StatusInternalServerError)
				return ctx.JSON(oidfed.ErrorServerError("internal server error"))
			}
			ctx.Set(fiber.HeaderContentType, oidfedconst.ContentTypeEntityStatement)
			return ctx.Send(jwt)
		},
	)
}
