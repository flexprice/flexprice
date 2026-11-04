WITH
	{p0:Array(Decimal256(20))} AS r_usage,
	{p1:Array(Decimal256(20))} AS r_utilized,
	{p2:Array(Decimal256(20))} AS r_overage,
	{p3:Array(Decimal256(20))} AS r_true_up,
	{p4:Array(Decimal256(20))} AS r_discount,
	{p5:Array(Decimal256(20))} AS r_free
SELECT page_row, w, col,
	toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_usage, idx)), 18), 18) AS a_usage,
	toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_utilized, idx)), 18), 18) AS a_utilized,
	toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_overage, idx)), 18), 18) AS a_overage,
	toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_true_up, idx)), 18), 18) AS a_true_up,
	toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_discount, idx)), 18), 18) AS a_discount,
	sum(q) AS quantity, sum(n) AS events
FROM (
	SELECT if(indexOf({p6:Array(String)}, rk) = 0, toUInt32(4294967295), toUInt32(indexOf({p6:Array(String)}, rk))) AS page_row, w, col, idx, toDecimal128(sum(qty_total), 15) AS q, count() AS n
	FROM (
		SELECT concat('', e.p_617070) AS rk, concat('', e.p_617070) AS row_text, e.bucket AS w, e.p_74797065 AS col, r.idx AS idx, e.qty_total AS qty_total
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p8:String}) AS p_74797065, JSONExtractString(properties, {p9:String}) AS p_617070, toStartOfDay(timestamp, 'UTC') AS bucket
			FROM meter_usage 
			WHERE tenant_id = {p11:String} AND environment_id = {p12:String} AND external_customer_id IN {p13:Array(String)}
				AND meter_id IN {p7:Array(String)} AND timestamp >= {p14:DateTime('UTC')} AND timestamp < {p15:DateTime('UTC')} AND ingested_at <= {p10:DateTime64(3, 'UTC')}
			AND JSONExtractString(properties, {p16:String}) NOT IN {p17:Array(String)}
			AND source IN {p18:Array(String)}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p19:Array(String)}, {p20:Array(DateTime('UTC'))}, {p21:Array(DateTime('UTC'))}, {p22:Array(UInt32)}, {p23:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
		UNION ALL
		SELECT concat('', e.p_617070) AS rk, concat('', e.p_617070) AS row_text, e.bucket AS w, e.p_74797065 AS col, r.idx AS idx, e.qty_total AS qty_total
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p25:String}) AS p_74797065, JSONExtractString(properties, {p26:String}) AS p_617070, toStartOfDay(timestamp, 'UTC') AS bucket
			FROM meter_usage 
			WHERE tenant_id = {p28:String} AND environment_id = {p29:String} AND external_customer_id IN {p30:Array(String)}
				AND meter_id IN {p24:Array(String)} AND timestamp >= {p31:DateTime('UTC')} AND timestamp < {p32:DateTime('UTC')} AND ingested_at <= {p27:DateTime64(3, 'UTC')}
			AND JSONExtractString(properties, {p33:String}) NOT IN {p34:Array(String)}
			AND source IN {p35:Array(String)}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p36:Array(String)}, {p37:Array(DateTime('UTC'))}, {p38:Array(DateTime('UTC'))}, {p39:Array(UInt32)}, {p40:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
	)
	WHERE positionCaseInsensitiveUTF8(row_text, {p41:String}) > 0
	GROUP BY page_row, w, col, idx
	UNION ALL
	SELECT if(indexOf({p6:Array(String)}, rk) = 0, toUInt32(4294967295), toUInt32(indexOf({p6:Array(String)}, rk))) AS page_row, w, col, idx, q, n FROM (
	SELECT cell.1 AS rk, cell.2 AS row_text, cell.3 AS w, cell.4 AS col, idx, q, n FROM (
SELECT m.1 AS cell, idx, if(m.1 = owner, toDecimal128(peak, 15), toDecimal128(0, 15)) AS q, m.2 AS n
FROM (
	SELECT idx, lbucket, grp, argMax(key, rq) AS owner, max(rq) AS peak, groupArray((key, rn)) AS members
	FROM (
		SELECT idx, lbucket, grp, (rk, row_text, w, col, pass) AS key, max(qty_total) AS rq, countIf(pass) AS rn
		FROM (
		SELECT concat('', e.p_617070) AS rk, concat('', e.p_617070) AS row_text, e.bucket AS w, e.p_74797065 AS col, (1
			AND e.p_617070 NOT IN {p43:Array(String)}
			AND e.source IN {p44:Array(String)}) AS pass, e.lbucket AS lbucket, e.p_74797065 AS grp, r.idx AS idx, e.qty_total AS qty_total
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p45:String}) AS p_74797065, JSONExtractString(properties, {p46:String}) AS p_617070, toStartOfDay(timestamp, 'UTC') AS bucket, toStartOfDay(timestamp, 'UTC') AS lbucket
			FROM meter_usage 
			WHERE tenant_id = {p48:String} AND environment_id = {p49:String} AND external_customer_id IN {p50:Array(String)}
				AND meter_id IN {p42:Array(String)} AND timestamp >= {p51:DateTime('UTC')} AND timestamp < {p52:DateTime('UTC')} AND ingested_at <= {p47:DateTime64(3, 'UTC')}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p53:Array(String)}, {p54:Array(DateTime('UTC'))}, {p55:Array(DateTime('UTC'))}, {p56:Array(UInt32)}, {p57:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
		)
		GROUP BY idx, lbucket, grp, key
	)
	GROUP BY idx, lbucket, grp
)
ARRAY JOIN members AS m
WHERE tupleElement(m.1, 5)
	))
	WHERE positionCaseInsensitiveUTF8(row_text, {p58:String}) > 0
)
GROUP BY GROUPING SETS ((page_row, w, col), (w, col))
HAVING page_row != 4294967295
SETTINGS max_memory_usage = 96636764160

-- parameters
-- p0 = [2,1.5,0,0.1,12]
-- p1 = [0,0,3,0,0]
-- p2 = [0,0,0,0.05,0]
-- p3 = [0,0,0.5,0,0]
-- p4 = [0.2,0,0,0,0]
-- p5 = [0.25,0,0,0,0]
-- p6 = ['promo','launch']
-- p7 = ['ai','compute']
-- p8 = type
-- p9 = app
-- p10 = 2026-08-31 00:00:00.000
-- p11 = tenant
-- p12 = env
-- p13 = ['cust']
-- p14 = 2026-08-01 00:00:00
-- p15 = 2026-08-31 00:00:00
-- p16 = app
-- p17 = ['payout']
-- p18 = ['api']
-- p19 = ['ai','compute','ai']
-- p20 = ['2026-08-01 00:00:00','2026-08-01 00:00:00','2026-08-15 12:00:00']
-- p21 = ['2026-08-15 12:00:00','2026-08-31 00:00:00','2026-08-31 00:00:00']
-- p22 = [1,3,2]
-- p23 = ['li_ai_old','li_compute_a','li_ai_new']
-- p24 = ['compute']
-- p25 = type
-- p26 = app
-- p27 = 2026-08-31 00:00:00.000
-- p28 = tenant
-- p29 = env
-- p30 = ['cust']
-- p31 = 2026-08-01 00:00:00
-- p32 = 2026-08-31 00:00:00
-- p33 = app
-- p34 = ['payout']
-- p35 = ['api']
-- p36 = ['compute']
-- p37 = ['2026-08-01 00:00:00']
-- p38 = ['2026-08-31 00:00:00']
-- p39 = [4]
-- p40 = ['li_compute_b']
-- p41 = pro
-- p42 = ['gpu']
-- p43 = ['payout']
-- p44 = ['api']
-- p45 = type
-- p46 = app
-- p47 = 2026-08-31 00:00:00.000
-- p48 = tenant
-- p49 = env
-- p50 = ['cust']
-- p51 = 2026-08-01 00:00:00
-- p52 = 2026-08-31 00:00:00
-- p53 = ['gpu']
-- p54 = ['2026-08-01 00:00:00']
-- p55 = ['2026-08-31 00:00:00']
-- p56 = [5]
-- p57 = ['li_gpu']
-- p58 = pro
