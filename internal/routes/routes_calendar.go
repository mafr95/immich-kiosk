package routes

import (
	"net/http"

	"charm.land/log/v2"
	"github.com/labstack/echo/v5"

	"github.com/damongolding/immich-kiosk/internal/calendar"
	"github.com/damongolding/immich-kiosk/internal/config"
	"github.com/damongolding/immich-kiosk/internal/templates/partials"
)

// Calendar calendar endpoint
func Calendar(baseConfig *config.Config) echo.HandlerFunc {
	return func(c *echo.Context) error {
		requestData, err := InitializeRequestData(c, baseConfig)
		if err != nil {
			return err
		}

		if requestData == nil {
			log.Info("Refreshing clients")
			return nil
		}

		requestConfig := requestData.RequestConfig
		requestID := requestData.RequestID

		if len(requestConfig.Calendar.Calendars) == 0 {
			log.Warn("No calendars configured")
			return c.NoContent(http.StatusNoContent)
		}

		events := calendar.CurrentEvents(requestConfig.Calendar.MaxEvents)

		log.Debug(
			requestID,
			"method", c.Request().Method,
			"path", c.Request().URL.String(),
			"events", len(events),
		)

		return Render(c, http.StatusOK, partials.CalendarEvents(events, requestConfig.SystemLang))
	}
}
