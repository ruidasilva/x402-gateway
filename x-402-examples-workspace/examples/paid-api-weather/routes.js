/**
 * Weather API — 3 sats per request
 *
 * GET /api/weather/:city
 *
 * Returns simulated weather data for any city.
 * Payment-gated at 3 satoshis per request.
 */

const express = require("express");
const { x402Middleware } = require("../shared/x402-middleware");

const router = express.Router();

const WEATHER_DATA = {
  london: { temp: 12, condition: "Cloudy", humidity: 78, wind_kph: 15 },
  tokyo: { temp: 22, condition: "Sunny", humidity: 55, wind_kph: 8 },
  "new-york": { temp: 18, condition: "Partly Cloudy", humidity: 62, wind_kph: 12 },
  sydney: { temp: 26, condition: "Clear", humidity: 45, wind_kph: 20 },
  paris: { temp: 14, condition: "Rain", humidity: 85, wind_kph: 10 },
  berlin: { temp: 10, condition: "Overcast", humidity: 72, wind_kph: 18 },
  lisbon: { temp: 20, condition: "Sunny", humidity: 50, wind_kph: 14 },
  dubai: { temp: 38, condition: "Clear", humidity: 30, wind_kph: 6 },
};

router.get(
  "/api/weather/:city",
  x402Middleware({ price: 3, description: "Weather data — 3 sats" }),
  (req, res) => {
    const city = req.params.city.toLowerCase();
    const data = WEATHER_DATA[city] || {
      temp: Math.floor(Math.random() * 35) + 5,
      condition: "Variable",
      humidity: Math.floor(Math.random() * 60) + 30,
      wind_kph: Math.floor(Math.random() * 25) + 5,
    };

    res.json({
      city: req.params.city,
      ...data,
      unit: "celsius",
      timestamp: new Date().toISOString(),
      payment: req.x402,
    });
  }
);

module.exports = router;
