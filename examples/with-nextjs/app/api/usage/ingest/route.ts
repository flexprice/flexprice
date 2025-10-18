import { NextResponse } from "next/server";
import {
    initFlexpriceClient,
    wrapCustomersPost,
    wrapEventsPost,
} from "../../../../lib/flexprice";

export async function POST(req: Request) {
    try {
        const body = await req.json();
        const external_customer_id: string =
            body.external_customer_id || "sample-customer";
        const event_name: string = body.event_name || "text_generation";
        const units: number = Number(body.units ?? 100);
        const unit_cost: number = Number(
            body.unit_cost ??
                (process.env.UNIT_COST ? Number(process.env.UNIT_COST) : 0.02)
        );

        const { customersApi, eventsApi } = initFlexpriceClient();

        // Create customer if not exists (idempotent by external id)
        try {
            await wrapCustomersPost(customersApi, {
                externalId: external_customer_id,
                name: external_customer_id,
                email: `${external_customer_id}@example.com`,
                metadata: { source: "nextjs_example" },
            });
        } catch (e) {
            // If already exists, ignore
            console.warn("customersPost warning (ignored)", e);
        }

        const eventRequest = {
            event_name,
            external_customer_id,
            properties: {
                units,
                unit_cost,
                cost: Number((units * unit_cost).toFixed(6)),
                source: "nextjs_example",
            },
            source: "nextjs_example",
            timestamp: new Date().toISOString(),
        };

        const result = await wrapEventsPost(eventsApi, eventRequest as any);

        return NextResponse.json({ ok: true, result });
    } catch (err: any) {
        console.error("/api/usage/ingest error", err);
        return new NextResponse(`Error: ${err?.message || "unknown"}`, {
            status: 500,
        });
    }
}
