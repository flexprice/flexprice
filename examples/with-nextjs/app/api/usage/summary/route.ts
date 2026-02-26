import { NextResponse } from "next/server";
import { initFlexpriceClient, wrapEventsGet } from "../../../../lib/flexprice";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
    try {
        const { searchParams } = new URL(req.url);
        const external_customer_id =
            searchParams.get("external_customer_id") || "sample-customer";

        const { eventsApi } = initFlexpriceClient();

        const data = await wrapEventsGet(eventsApi, { external_customer_id });

        const events = Array.isArray((data as any)?.events)
            ? (data as any).events
            : [];

        const unitCostEnv = process.env.UNIT_COST
            ? Number(process.env.UNIT_COST)
            : 0.02;

        let total_units = 0;
        let total_cost = 0;

        const recent = events.slice(0, 25);

        for (const e of events) {
            const units = Number(e?.properties?.units ?? 1);
            const cost = Number(e?.properties?.cost ?? units * unitCostEnv);
            total_units += Number.isFinite(units) ? units : 0;
            total_cost += Number.isFinite(cost) ? cost : 0;
        }

        return NextResponse.json({
            external_customer_id,
            total_events: events.length,
            total_units,
            total_cost: Number(total_cost.toFixed(4)),
            currency: "USD",
            recent_events: recent.map((e: any) => ({
                id: e.id || e.event_id || "unknown",
                event_name: e.event_name,
                timestamp: e.timestamp,
                properties: e.properties,
            })),
        });
    } catch (err: any) {
        console.error("/api/usage/summary error", err);
        return new NextResponse(`Error: ${err?.message || "unknown"}`, {
            status: 500,
        });
    }
}
