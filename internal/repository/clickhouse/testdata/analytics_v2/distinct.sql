SELECT indexOf({p0:Array(String)}, rk) AS page_row, uniqExact(d0), groupUniqArray(6)(d0), uniqExact(d1), groupUniqArray(6)(d1)
FROM (
		SELECT concat('', e.p_617070) AS rk, concat('', e.p_617070) AS row_text, e.source AS d0, e.p_74797065 AS d1, r.idx AS idx
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p2:String}) AS p_617070, JSONExtractString(properties, {p3:String}) AS p_74797065
			FROM meter_usage 
			WHERE tenant_id = {p5:String} AND environment_id = {p6:String} AND external_customer_id IN {p7:Array(String)}
				AND meter_id IN {p1:Array(String)} AND timestamp >= {p8:DateTime('UTC')} AND timestamp < {p9:DateTime('UTC')} AND ingested_at <= {p4:DateTime64(3, 'UTC')}
			AND JSONExtractString(properties, {p10:String}) NOT IN {p11:Array(String)}
			AND source IN {p12:Array(String)}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p13:Array(String)}, {p14:Array(DateTime('UTC'))}, {p15:Array(DateTime('UTC'))}, {p16:Array(UInt32)}, {p17:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
		UNION ALL
		SELECT concat('', e.p_617070) AS rk, concat('', e.p_617070) AS row_text, e.source AS d0, e.p_74797065 AS d1, r.idx AS idx
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p19:String}) AS p_617070, JSONExtractString(properties, {p20:String}) AS p_74797065
			FROM meter_usage 
			WHERE tenant_id = {p22:String} AND environment_id = {p23:String} AND external_customer_id IN {p24:Array(String)}
				AND meter_id IN {p18:Array(String)} AND timestamp >= {p25:DateTime('UTC')} AND timestamp < {p26:DateTime('UTC')} AND ingested_at <= {p21:DateTime64(3, 'UTC')}
			AND JSONExtractString(properties, {p27:String}) NOT IN {p28:Array(String)}
			AND source IN {p29:Array(String)}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p30:Array(String)}, {p31:Array(DateTime('UTC'))}, {p32:Array(DateTime('UTC'))}, {p33:Array(UInt32)}, {p34:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
)
WHERE indexOf({p0:Array(String)}, rk) > 0
GROUP BY page_row
SETTINGS max_memory_usage = 96636764160

-- parameters
-- p0 = ['promo','launch']
-- p1 = ['ai','compute','gpu']
-- p2 = app
-- p3 = type
-- p4 = 2026-08-31 00:00:00.000
-- p5 = tenant
-- p6 = env
-- p7 = ['cust']
-- p8 = 2026-08-01 00:00:00
-- p9 = 2026-08-31 00:00:00
-- p10 = app
-- p11 = ['payout']
-- p12 = ['api']
-- p13 = ['ai','compute','gpu','ai']
-- p14 = ['2026-08-01 00:00:00','2026-08-01 00:00:00','2026-08-01 00:00:00','2026-08-15 12:00:00']
-- p15 = ['2026-08-15 12:00:00','2026-08-31 00:00:00','2026-08-31 00:00:00','2026-08-31 00:00:00']
-- p16 = [1,3,5,2]
-- p17 = ['li_ai_old','li_compute_a','li_gpu','li_ai_new']
-- p18 = ['compute']
-- p19 = app
-- p20 = type
-- p21 = 2026-08-31 00:00:00.000
-- p22 = tenant
-- p23 = env
-- p24 = ['cust']
-- p25 = 2026-08-01 00:00:00
-- p26 = 2026-08-31 00:00:00
-- p27 = app
-- p28 = ['payout']
-- p29 = ['api']
-- p30 = ['compute']
-- p31 = ['2026-08-01 00:00:00']
-- p32 = ['2026-08-31 00:00:00']
-- p33 = [4]
-- p34 = ['li_compute_b']
