"use client";

import { useEffect, useMemo, useState, useTransition } from "react";
import CostCard from "../components/CostCard";
import EventsTable from "../components/EventsTable";

interface Summary {
    external_customer_id: string;
    total_events: number;
    total_units: number;
    total_cost: number;
    currency: string;
    recent_events: Array<{
        id: string;
        event_name: string;
        timestamp?: string;
        properties?: Record<string, any>;
    }>;
}

export default function Page() {
    const [customerId, setCustomerId] = useState("sample-customer");
    const [summary, setSummary] = useState<Summary | null>(null);
    const [isPending, startTransition] = useTransition();
    const [busy, setBusy] = useState(false);

    const formattedCost = useMemo(
        () =>
            summary
                ? new Intl.NumberFormat(undefined, {
                      style: "currency",
                      currency: summary.currency,
                  }).format(summary.total_cost)
                : "-",
        [summary]
    );

    async function load() {
        const res = await fetch(
            `/api/usage/summary?external_customer_id=${encodeURIComponent(
                customerId
            )}`,
            { cache: "no-store" }
        );
        if (!res.ok) throw new Error("Failed to load summary");
        const json = await res.json();
        setSummary(json);
    }

    useEffect(() => {
        startTransition(() => {
            load().catch(console.error);
        });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [customerId]);

    async function ingestSample() {
        setBusy(true);
        try {
            const body = {
                external_customer_id: customerId,
                event_name: "text_generation",
                units: Math.floor(Math.random() * 800) + 200, // e.g. tokens
                unit_cost: Number(process.env.NEXT_PUBLIC_UNIT_COST) || 0.02,
            };
            const res = await fetch("/api/usage/ingest", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(body),
            });
            if (!res.ok) throw new Error(await res.text());
            await load();
        } catch (e) {
            console.error(e);
            alert(
                "Failed to ingest sample event. Check server logs and your FLEXPRICE envs."
            );
        } finally {
            setBusy(false);
        }
    }

    return (
        <section className="grid gap-16">
            <div className="controls">
                <label>
                    External Customer ID
                    <input
                        value={customerId}
                        onChange={(e) => setCustomerId(e.target.value)}
                        placeholder="sample-customer"
                    />
                </label>
                <button onClick={ingestSample} disabled={busy}>
                    {busy ? "Sending…" : "Ingest sample usage"}
                </button>
            </div>

            <div className="cards">
                <CostCard
                    title="Total cost"
                    value={formattedCost}
                    subtitle="Based on ingested events"
                    loading={isPending && !summary}
                />
                <CostCard
                    title="Total events"
                    value={summary?.total_events ?? 0}
                    subtitle="Events in Flexprice"
                    loading={isPending && !summary}
                />
                <CostCard
                    title="Total units"
                    value={summary?.total_units ?? 0}
                    subtitle="Sum of units property"
                    loading={isPending && !summary}
                />
            </div>

            <article>
                <h2>Recent events</h2>
                <EventsTable events={summary?.recent_events ?? []} />
            </article>
        </section>
    );
}
