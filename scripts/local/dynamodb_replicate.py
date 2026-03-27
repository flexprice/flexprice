import pandas as pd
import clickhouse_connect
import logging
import json
import numpy as np

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
        "database": "flexprice",
    },
}

# File path for the DynamoDB export
dynamo_csv_path = 'dynamo_export.csv'

def fix_and_insert_data(config, csv_path):
    logger.info("Reading DynamoDB export data...")
    data = pd.read_csv(csv_path)

    # Handle column renaming
    column_mapping = {
        'pk': 'id',
        'sk': 'tenant_id'
    }
    data.rename(columns=column_mapping, inplace=True)
    logger.info(f"Renamed columns: {column_mapping}")
    
    # Replace NaN values with empty strings
    logger.info("Replacing NaN values with empty strings...")
    data.fillna('', inplace=True)

    # Ensure string fields are strings
    string_columns = ["id", "source", "properties", "customer_id", "tenant_id", "external_customer_id", "event_name"]
    for col in string_columns:
        if col in data.columns:
            data[col] = data[col].astype(str)

    # Fix timestamp fields to strict ISO 8601 with millisecond precision
    logger.info("Processing timestamps...")
    for col in ['timestamp', 'ingested_at']:
        if col in data.columns:
            data[col] = (
                pd.to_datetime(data[col], errors='coerce')
                .dt.strftime('%Y-%m-%dT%H:%M:%S.%f')
                .str[:-3]  # Trim to milliseconds
            )
            # Replace NaN or invalid timestamps with empty strings
            data[col] = data[col].fillna('')

    # Ensure properties are valid JSON strings
    logger.info("Validating JSON properties...")
    if 'properties' in data.columns:
        data['properties'] = data['properties'].apply(lambda x: x if isinstance(x, str) else '{}')

    # Ensure the data matches ClickHouse schema
    columns = [
        "id", "source", "properties", "customer_id",
        "ingested_at", "timestamp", "tenant_id",
        "external_customer_id", "event_name"
    ]
    data = data[columns]  # Reorder columns to match schema

    # Debug exact payload for first row
    first_row = data.iloc[0].to_dict()
    logger.info("First row being inserted:")
    logger.info(json.dumps(first_row, indent=4))

    # Connect to ClickHouse
    logger.info("Connecting to ClickHouse...")
    client = clickhouse_connect.get_client(
        host=config['clickhouse']['address'].split(':')[0],
        port=int(config['clickhouse']['address'].split(':')[1]),
        username=config['clickhouse']['username'],
        password=config['clickhouse']['password'],
        database=config['clickhouse']['database']
    )

    # Insert data in batches using query-based insert
    logger.info("Inserting data into ClickHouse using queries...")
    try:
        batch_size = 1000
        for i in range(0, len(data), batch_size):
            batch = data.iloc[i:i + batch_size]
            values = [
                (
                    row['id'], row['source'], row['properties'], row['customer_id'],
                    row['ingested_at'], row['timestamp'], row['tenant_id'],
                    row['external_customer_id'], row['event_name']
                )
                for _, row in batch.iterrows()
            ]
            
            query = f"""
                INSERT INTO {config['clickhouse']['database']}.events (
                    id, source, properties, customer_id, ingested_at, timestamp,
                    tenant_id, external_customer_id, event_name
                )
                VALUES {', '.join(str(value) for value in values)}
            """
            client.command(query)
        
        logger.info(f"Inserted {len(data)} rows into {config['clickhouse']['database']}.events")
    except Exception as e:
        logger.error(f"Error during insertion: {e}")
        logger.error("Exact payload for first row:")
        logger.error(json.dumps(first_row, indent=4))
        raise


if __name__ == "__main__":
    try:
        fix_and_insert_data(CONFIG, dynamo_csv_path)
        logger.info("Data ingestion completed successfully.")
    except Exception as e:
        logger.error(f"An error occurred: {e}")
