import argparse
import logging
from flexprice_ingest import seed_events_clickhouse
from clickhouse_query_load import monitor_queries

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Hardcoded configuration
CONFIG = {
    "clickhouse": {
        "address": "",
        "tls": False,
        "username": "",
        "password": "",
        "database": "",
    },
}


def main():
    parser = argparse.ArgumentParser(description="ClickHouse Ingestion and Query Monitoring")
    parser.add_argument("--mode", choices=["ingest", "query"], default="ingest", help="Operation mode")
    args = parser.parse_args()

    if args.mode == "ingest":
        logger.info("Running in ingest mode...")
        seed_events_clickhouse(CONFIG)
    elif args.mode == "query":
        logger.info("Running in query mode...")
        monitor_queries(CONFIG)
    else:
        logger.error("Invalid mode specified")


if __name__ == "__main__":
    main()
