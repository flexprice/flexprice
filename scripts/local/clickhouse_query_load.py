import logging
import time
import clickhouse_connect


# Set up logging
logger = logging.getLogger(__name__)
logger.setLevel(logging.INFO)

# File handler to save logs to a file
file_handler = logging.FileHandler("ingestion.log" if __name__ == "__main__" else "query_monitor.log")
file_handler.setLevel(logging.INFO)

# Formatter for log messages
formatter = logging.Formatter("%(asctime)s - %(levelname)s - %(message)s")
file_handler.setFormatter(formatter)

# Stream handler to print logs to the console (optional)
stream_handler = logging.StreamHandler()
stream_handler.setLevel(logging.INFO)
stream_handler.setFormatter(formatter)

# Add handlers to the logger
logger.addHandler(file_handler)
logger.addHandler(stream_handler)


# Query interval in seconds
QUERY_INTERVAL = 0.01


def monitor_queries(config):
    """
    Monitor queries to ClickHouse and log results.
    """
    logger.info("Connecting to ClickHouse...")
    client = clickhouse_connect.get_client(
        host=config["clickhouse"]["address"].split(":")[0],
        port=8123,
        username=config["clickhouse"]["username"],
        password=config["clickhouse"]["password"],
        database=config["clickhouse"]["database"],
        secure=config["clickhouse"]["tls"],
    )

    logger.info("Starting query monitoring...")
    while True:
        try:
            # Example storage query
            storage_query = """
            SELECT sum(value) as total, count(*) as count
            FROM (
                SELECT
                    anyLast(JSONExtractFloat(properties, 'bytes_transferred')) as value
                FROM events
                WHERE event_name = 'gpu_time' 
            ) limit 1
            """
            storage_result = client.query(storage_query)
            row = storage_result.result_rows[0][0]
            logger.info(f"Storage Query Result: {row}")

            ingestion_lag_query = """
            SELECT
                toStartOfMinute(timestamp) AS minute,
                COUNT(*) AS events_per_minute,
                MIN(dateDiff('millisecond', timestamp, ingested_at)) AS min_latency_ms,
                MAX(dateDiff('millisecond', timestamp, ingested_at)) AS max_latency_ms,
                AVG(dateDiff('millisecond', timestamp, ingested_at)) AS avg_latency_ms
            FROM flexprice.events
            WHERE timestamp >= NOW() - INTERVAL 1 HOUR
            GROUP BY minute
            ORDER BY minute DESC
            """
            ingestion_lag = client.query(ingestion_lag_query)
            logger.info(f"Ingestion Lag Query Result: {ingestion_lag.result_rows}")



            summarise_query = """
            SELECT 
                 sum(value) as total,  tenant_id, external_customer_id, customer_id, event_name
            FROM (
            SELECT
                 JSONExtractFloat(assumeNotNull(properties), 'duration_ms') as value,
                  tenant_id, external_customer_id, customer_id, event_name
            FROM flexprice.events
                
                 ) GROUP BY tenant_id, external_customer_id, customer_id, event_name
            """
            summarise = client.query(summarise_query)
            logger.info(f"Other Query Result: {summarise.result_rows}")


            event_count_query = """
            SELECT count(*) as count
            FROM events
            WHERE event_name = 'gpu_time'
            """
            event_count = client.query(event_count_query)
            logger.info(f"Other Query Result: {event_count.result_rows}")

            # Example duration query
            duration_query = """
            SELECT avg(duration) as avg_duration, max(duration) as max_duration
            FROM (
                SELECT
                    anyLast(JSONExtractFloat(properties, 'duration_ms')) as duration
                FROM events
                WHERE event_name = 'gpu_time'
            )
            """
            duration_result = client.query(duration_query)
            logger.info(f"Duration Query Result: {duration_result.result_rows}")

        except Exception as e:
            logger.error(f"Failed to execute queries: {e}")
        time.sleep(QUERY_INTERVAL)
