2025/09/10 08:34:39 Executing query for SUM_WITH_MULTIPLIER: 
        SELECT 
             (sum(value) * 1.000000) as total
        FROM (
            SELECT
                 anyLast(JSONExtractFloat(assumeNotNull(properties), 'data')) as value
            FROM events
            PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
                                AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
                                AND event_name = 'api_sum_mul'
                                AND external_customer_id = 'cust-customer-1'

                
                AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
            GROUP BY id 
        )
        
    
2025/09/10 08:34:39 Executing query for LATEST: 
        SELECT 
             argMax(JSONExtractFloat(assumeNotNull(properties), 'data'), timestamp) as total
        FROM 
                        events  PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
                AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
                AND event_name = 'api_latest'
                AND external_customer_id = 'cust-customer-1'
                
                
                AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
        
    
2025/09/10 08:34:39 Executing query for MAX: 
                SELECT 
                         max(value) as total
                FROM (
                        SELECT
                                 anyLast(JSONExtractFloat(assumeNotNull(properties), 'data')) as value
                        FROM events
                        PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
                                AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
                                AND event_name = 'api_max'
                                AND external_customer_id = 'cust-customer-1'


                                AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
                        GROUP BY id 
                )


2025/09/10 08:34:39 Executing query for COUNT_UNIQUE: 
        SELECT 
             count(DISTINCT property_value) as total
        FROM (
            SELECT
                 JSONExtractString(assumeNotNull(properties), 'data') as property_value
            FROM events
            PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
                                AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
                                AND event_name = 'api_count_uq'
                                AND external_customer_id = 'cust-customer-1'

                
                AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
            GROUP BY id, property_value 
        )
        
    
2025/09/10 08:34:39 Executing query for COUNT: 
        SELECT 
             count(DISTINCT id) as total
        FROM events
        PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
                        AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
                        AND event_name = 'api_counts'
                        AND external_customer_id = 'cust-customer-1'
            AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
        
    
2025-09-10T08:34:39.116Z        DEBUG   service/event.go:293    completed meter usage request   {"meter_id": "meter_01K4SBQ6X82WT7K12HY4TBGHWG", "price_id": "price_01K4SBVYK0FMYKNQ59QCJKMGWR", "meter_index": 1, "processing_time_ms": 86}
2025-09-10T08:34:39.116Z        DEBUG   service/event.go:243    starting meter usage request    {"meter_id": "meter_01K4SBJ82BATGKHK4KC2QF37GG", "price_id": "price_01K4SBVYK0FMYKNQ59Q2T33ZB6", "meter_index": 5}
2025/09/10 08:34:39 Executing query for SUM: 
        SELECT 
             sum(value) as total
        FROM (
            SELECT
                 anyLast(JSONExtractFloat(assumeNotNull(properties), 'data')) as value
            FROM events
            PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
                                AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
                                AND event_name = 'api_sum'
                                AND external_customer_id = 'cust-customer-1'

                
                AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
            GROUP BY id 
        )
        
    