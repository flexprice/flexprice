SELECT idx, sum(quantity), sum(range_quantity), sum(events) FROM (
	SELECT idx, sum(qty_total) AS quantity, sumIf(qty_total, ts >= {p0:DateTime('UTC')} AND ts < {p1:DateTime('UTC')}) AS range_quantity, count() AS events
	FROM (
		SELECT r.idx AS idx, e.qty_total AS qty_total, e.timestamp AS ts
		FROM (
			SELECT meter_id, timestamp, qty_total, source
			FROM meter_usage 
			WHERE tenant_id = {p4:String} AND environment_id = {p5:String} AND external_customer_id IN {p6:Array(String)}
				AND meter_id IN {p2:Array(String)} AND timestamp >= {p7:DateTime('UTC')} AND timestamp < {p8:DateTime('UTC')} AND ingested_at <= {p3:DateTime64(3, 'UTC')}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p9:Array(String)}, {p10:Array(DateTime('UTC'))}, {p11:Array(DateTime('UTC'))}, {p12:Array(UInt32)}, {p13:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
		UNION ALL
		SELECT r.idx AS idx, e.qty_total AS qty_total, e.timestamp AS ts
		FROM (
			SELECT meter_id, timestamp, qty_total, source
			FROM meter_usage 
			WHERE tenant_id = {p16:String} AND environment_id = {p17:String} AND external_customer_id IN {p18:Array(String)}
				AND meter_id IN {p14:Array(String)} AND timestamp >= {p19:DateTime('UTC')} AND timestamp < {p20:DateTime('UTC')} AND ingested_at <= {p15:DateTime64(3, 'UTC')}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p21:Array(String)}, {p22:Array(DateTime('UTC'))}, {p23:Array(DateTime('UTC'))}, {p24:Array(UInt32)}, {p25:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
	)
	GROUP BY idx
	UNION ALL
	SELECT idx, sum(m) AS quantity, sum(mr) AS range_quantity, sum(n) AS events FROM (
		SELECT idx, lbucket, grp, max(qty_total) AS m, maxIf(qty_total, ts >= {p26:DateTime('UTC')} AND ts < {p27:DateTime('UTC')}) AS mr, count() AS n
		FROM (
		SELECT e.lbucket AS lbucket, e.p_74797065 AS grp, r.idx AS idx, e.qty_total AS qty_total, e.timestamp AS ts
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p29:String}) AS p_74797065, toStartOfDay(timestamp, 'UTC') AS lbucket
			FROM meter_usage 
			WHERE tenant_id = {p31:String} AND environment_id = {p32:String} AND external_customer_id IN {p33:Array(String)}
				AND meter_id IN {p28:Array(String)} AND timestamp >= {p34:DateTime('UTC')} AND timestamp < {p35:DateTime('UTC')} AND ingested_at <= {p30:DateTime64(3, 'UTC')}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p36:Array(String)}, {p37:Array(DateTime('UTC'))}, {p38:Array(DateTime('UTC'))}, {p39:Array(UInt32)}, {p40:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
		)
		GROUP BY idx, lbucket, grp
	)
	GROUP BY idx
)
GROUP BY idx
SETTINGS max_memory_usage = 96636764160

-- parameters
-- p0 = 2026-08-10 00:00:00
-- p1 = 2026-08-20 00:00:00
-- p2 = ['ai','compute']
-- p3 = 2026-08-31 00:00:00.000
-- p4 = tenant
-- p5 = env
-- p6 = ['cust']
-- p7 = 2026-08-01 00:00:00
-- p8 = 2026-08-31 00:00:00
-- p9 = ['ai','compute','ai']
-- p10 = ['2026-08-01 00:00:00','2026-08-01 00:00:00','2026-08-15 12:00:00']
-- p11 = ['2026-08-15 12:00:00','2026-08-31 00:00:00','2026-08-31 00:00:00']
-- p12 = [1,3,2]
-- p13 = ['li_ai_old','li_compute_a','li_ai_new']
-- p14 = ['compute']
-- p15 = 2026-08-31 00:00:00.000
-- p16 = tenant
-- p17 = env
-- p18 = ['cust']
-- p19 = 2026-08-01 00:00:00
-- p20 = 2026-08-31 00:00:00
-- p21 = ['compute']
-- p22 = ['2026-08-01 00:00:00']
-- p23 = ['2026-08-31 00:00:00']
-- p24 = [4]
-- p25 = ['li_compute_b']
-- p26 = 2026-08-10 00:00:00
-- p27 = 2026-08-20 00:00:00
-- p28 = ['gpu']
-- p29 = type
-- p30 = 2026-08-31 00:00:00.000
-- p31 = tenant
-- p32 = env
-- p33 = ['cust']
-- p34 = 2026-08-01 00:00:00
-- p35 = 2026-08-31 00:00:00
-- p36 = ['gpu']
-- p37 = ['2026-08-01 00:00:00']
-- p38 = ['2026-08-31 00:00:00']
-- p39 = [5]
-- p40 = ['li_gpu']
