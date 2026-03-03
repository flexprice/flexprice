#!/usr/bin/env python3
"""
Flight Availability Monitor for Air India Express IX 182
AUH (Abu Dhabi) → DEL (Delhi) | Wed, Mar 04, 2026 | 13:40 → 19:15

Continuously checks Google Flights via fast-flights and reports
when the flight becomes available for booking.
"""

import time
import sys
import json
from datetime import datetime

from fast_flights import FlightData, Passengers, get_flights

# ── Flight details to monitor ──────────────────────────────────────────
TARGET_FLIGHT = {
    "carrier": "Air India Express",
    "number": "IX 182",
    "from": "AUH",
    "to": "DEL",
    "date": "2026-03-04",
    "departure_time": "1:40 PM",
    "arrival_time": "7:15 PM",
    "duration_contains": "4 hr",
}

CHECK_INTERVAL_SECONDS = 300  # 5 minutes between checks
MAX_CHECKS = 288              # 288 * 5min = 24 hours max runtime
LOG_FILE = "/home/user/flexprice/flight_monitor.log"


def log(msg: str):
    """Log to both stdout and file."""
    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    line = f"[{timestamp}] {msg}"
    print(line, flush=True)
    with open(LOG_FILE, "a") as f:
        f.write(line + "\n")


def is_target_flight(flight) -> bool:
    """Check if a flight result matches our target IX 182."""
    name = getattr(flight, "name", "") or ""
    departure = getattr(flight, "departure", "") or ""
    arrival = getattr(flight, "arrival", "") or ""
    duration = getattr(flight, "duration", "") or ""

    # Must be Air India Express (not a codeshare with multiple carriers)
    if name.strip() != "Air India Express":
        return False

    # Must depart at 1:40 PM on Wed, Mar 4
    if "1:40 PM" not in departure or "Mar 4" not in departure:
        return False

    # Must arrive at 7:15 PM on Wed, Mar 4 (same day = nonstop)
    if "7:15 PM" not in arrival or "Mar 4" not in arrival:
        return False

    # Sanity: duration should be ~4 hours (nonstop)
    if "4 hr" not in duration:
        return False

    return True


def check_availability() -> dict:
    """
    Query Google Flights for AUH→DEL on 2026-03-04 and look for IX 182.
    Returns a dict with status info.
    """
    result = get_flights(
        flight_data=[
            FlightData(
                date=TARGET_FLIGHT["date"],
                from_airport=TARGET_FLIGHT["from"],
                to_airport=TARGET_FLIGHT["to"],
            )
        ],
        trip="one-way",
        seat="economy",
        passengers=Passengers(adults=1),
        fetch_mode="fallback",
    )

    for flight in result.flights:
        if is_target_flight(flight):
            price = getattr(flight, "price", None) or "Price unavailable"
            return {
                "found": True,
                "available": "unavailable" not in str(price).lower(),
                "price": str(price),
                "name": getattr(flight, "name", ""),
                "departure": getattr(flight, "departure", ""),
                "arrival": getattr(flight, "arrival", ""),
                "duration": getattr(flight, "duration", ""),
            }

    return {"found": False, "available": False, "price": None}


def main():
    log("=" * 70)
    log("FLIGHT AVAILABILITY MONITOR STARTED")
    log(f"  Flight:    {TARGET_FLIGHT['number']} ({TARGET_FLIGHT['carrier']})")
    log(f"  Route:     {TARGET_FLIGHT['from']} → {TARGET_FLIGHT['to']}")
    log(f"  Date:      {TARGET_FLIGHT['date']}")
    log(f"  Schedule:  {TARGET_FLIGHT['departure_time']} → {TARGET_FLIGHT['arrival_time']}")
    log(f"  Interval:  Every {CHECK_INTERVAL_SECONDS}s ({CHECK_INTERVAL_SECONDS // 60} min)")
    log(f"  Max checks: {MAX_CHECKS}")
    log("=" * 70)

    for check_num in range(1, MAX_CHECKS + 1):
        log(f"Check #{check_num}/{MAX_CHECKS} — querying Google Flights...")

        try:
            result = check_availability()

            if not result["found"]:
                log("  Flight IX 182 NOT FOUND in results. "
                    "The flight may have been removed from schedule.")
            elif result["available"]:
                log("!" * 70)
                log("  *** FLIGHT IS NOW AVAILABLE FOR BOOKING! ***")
                log(f"  Flight:    {result['name']}")
                log(f"  Departure: {result['departure']}")
                log(f"  Arrival:   {result['arrival']}")
                log(f"  Duration:  {result['duration']}")
                log(f"  Price:     {result['price']}")
                log("!" * 70)
                log("  Book now at: https://flights.airindiaexpress.com/en-ae/abu-dhabi-to-delhi-flights")
                log("  Or Google Flights: https://www.google.com/travel/flights")
                log("MONITOR COMPLETE — flight is available!")

                # Write a signal file for easy detection
                with open("/home/user/flexprice/flight_available.signal", "w") as f:
                    json.dump({
                        "status": "AVAILABLE",
                        "timestamp": datetime.now().isoformat(),
                        **result,
                    }, f, indent=2)

                return 0
            else:
                log(f"  Flight found but NOT YET BOOKABLE (price: {result['price']})")

        except Exception as e:
            log(f"  ERROR during check: {type(e).__name__}: {e}")

        if check_num < MAX_CHECKS:
            log(f"  Sleeping {CHECK_INTERVAL_SECONDS}s until next check...")
            time.sleep(CHECK_INTERVAL_SECONDS)

    log("MAX CHECKS REACHED — flight did not become available within monitoring window.")
    return 1


if __name__ == "__main__":
    sys.exit(main())
