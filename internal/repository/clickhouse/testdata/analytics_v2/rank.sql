WITH
	{p0:Array(Decimal256(20))} AS r_usage,
	{p1:Array(Decimal256(20))} AS r_utilized,
	{p2:Array(Decimal256(20))} AS r_overage,
	{p3:Array(Decimal256(20))} AS r_true_up,
	{p4:Array(Decimal256(20))} AS r_discount,
	{p5:Array(Decimal256(20))} AS r_free
SELECT k1,
	a_usage, a_utilized, a_overage, a_true_up, a_discount, a_free, quantity, events,
	lines, c_cost, c_quantity, c_events,
	count() OVER () AS total_rows,
	sum(a_usage) OVER (), sum(a_utilized) OVER (), sum(a_overage) OVER (), sum(a_true_up) OVER (),
	sum(a_discount) OVER (), sum(a_free) OVER (), sum(quantity) OVER (), sum(events) OVER (),
	sumMap(c_cost) OVER (), sumMap(c_quantity) OVER ()
FROM (
	SELECT k1,
		sum(b_usage) AS a_usage, sum(b_utilized) AS a_utilized, sum(b_overage) AS a_overage,
		sum(b_true_up) AS a_true_up, sum(b_discount) AS a_discount, sum(b_free) AS a_free,
		sum(b_quantity) AS quantity, sum(b_events) AS events,
		groupUniqArrayArray(b_lines) AS lines,
		sumMap(map(col, b_usage + b_utilized + b_overage + b_true_up - b_discount)) AS c_cost,
		sumMap(map(col, b_quantity)) AS c_quantity,
		sumMap(map(col, b_events)) AS c_events,
		sumIf(b_usage + b_utilized + b_overage + b_true_up - b_discount, col = {p6:String}) AS sort_column
	FROM (
		SELECT k1, col,
			toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_usage, idx)), 18), 18) AS b_usage,
			toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_utilized, idx)), 18), 18) AS b_utilized,
			toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_overage, idx)), 18), 18) AS b_overage,
			toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_true_up, idx)), 18), 18) AS b_true_up,
			toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_discount, idx)), 18), 18) AS b_discount,
			toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(r_free, idx)), 18), 18) AS b_free,
			sum(q) AS b_quantity, sum(n) AS b_events, groupUniqArray(idx) AS b_lines
		FROM (
			SELECT k1, col, idx, toDecimal128(sum(qty_total), 15) AS q, count() AS n
			FROM (
		SELECT e.p_617070 AS k1, e.p_74797065 AS col, r.idx AS idx, e.qty_total AS qty_total
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p8:String}) AS p_74797065, JSONExtractString(properties, {p9:String}) AS p_617070
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
		SELECT e.p_617070 AS k1, e.p_74797065 AS col, r.idx AS idx, e.qty_total AS qty_total
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p25:String}) AS p_74797065, JSONExtractString(properties, {p26:String}) AS p_617070
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
			GROUP BY k1, col, idx
	UNION ALL
			SELECT cell.1 AS k1, cell.2 AS col, idx, q, n FROM (
SELECT m.1 AS cell, idx, if(m.1 = owner, toDecimal128(peak, 15), toDecimal128(0, 15)) AS q, m.2 AS n
FROM (
	SELECT idx, lbucket, grp, argMax(key, rq) AS owner, max(rq) AS peak, groupArray((key, rn)) AS members
	FROM (
		SELECT idx, lbucket, grp, (k1, col, pass) AS key, max(qty_total) AS rq, countIf(pass) AS rn
		FROM (
		SELECT e.p_617070 AS k1, e.p_74797065 AS col, (1
			AND e.p_617070 NOT IN {p42:Array(String)}
			AND e.source IN {p43:Array(String)}) AS pass, e.lbucket AS lbucket, e.p_74797065 AS grp, r.idx AS idx, e.qty_total AS qty_total
		FROM (
			SELECT meter_id, timestamp, qty_total, source, JSONExtractString(properties, {p44:String}) AS p_74797065, JSONExtractString(properties, {p45:String}) AS p_617070, toStartOfDay(timestamp, 'UTC') AS lbucket
			FROM meter_usage 
			WHERE tenant_id = {p47:String} AND environment_id = {p48:String} AND external_customer_id IN {p49:Array(String)}
				AND meter_id IN {p41:Array(String)} AND timestamp >= {p50:DateTime('UTC')} AND timestamp < {p51:DateTime('UTC')} AND ingested_at <= {p46:DateTime64(3, 'UTC')}
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip({p52:Array(String)}, {p53:Array(DateTime('UTC'))}, {p54:Array(DateTime('UTC'))}, {p55:Array(UInt32)}, {p56:Array(String)})) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time
		)
		GROUP BY idx, lbucket, grp, key
	)
	GROUP BY idx, lbucket, grp
)
ARRAY JOIN members AS m
WHERE tupleElement(m.1, 3)
			)
		)
		GROUP BY k1, col
	)
	GROUP BY k1
)
WHERE positionCaseInsensitiveUTF8(concat(k1), {p57:String}) > 0
ORDER BY sort_column DESC, k1
LIMIT 10 OFFSET 0
SETTINGS max_memory_usage = 96636764160

-- parameters
-- p0 = [2,1.5,0,0.1,12]
-- p1 = [0,0,3,0,0]
-- p2 = [0,0,0,0.05,0]
-- p3 = [0,0,0.5,0,0]
-- p4 = [0.2,0,0,0,0]
-- p5 = [0.25,0,0,0,0]
-- p6 = run
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
-- p41 = ['gpu']
-- p42 = ['payout']
-- p43 = ['api']
-- p44 = type
-- p45 = app
-- p46 = 2026-08-31 00:00:00.000
-- p47 = tenant
-- p48 = env
-- p49 = ['cust']
-- p50 = 2026-08-01 00:00:00
-- p51 = 2026-08-31 00:00:00
-- p52 = ['gpu']
-- p53 = ['2026-08-01 00:00:00']
-- p54 = ['2026-08-31 00:00:00']
-- p55 = [5]
-- p56 = ['li_gpu']
-- p57 = pro
