type EventRow = {
    id: string;
    event_name: string;
    timestamp?: string;
    properties?: Record<string, any>;
};

export default function EventsTable({ events }: { events: EventRow[] }) {
    if (!events.length) {
        return (
            <div className="card">
                No events yet. Click “Ingest sample usage”.
            </div>
        );
    }

    return (
        <table className="table">
            <thead>
                <tr>
                    <th>ID</th>
                    <th>Event</th>
                    <th>Units</th>
                    <th>Cost</th>
                    <th>Timestamp</th>
                </tr>
            </thead>
            <tbody>
                {events.slice(0, 25).map((e) => (
                    <tr key={e.id}>
                        <td>
                            <code>{e.id}</code>
                        </td>
                        <td>{e.event_name}</td>
                        <td>{e.properties?.units ?? 1}</td>
                        <td>
                            {typeof e.properties?.cost === "number"
                                ? `$${e.properties?.cost.toFixed(4)}`
                                : "—"}
                        </td>
                        <td>
                            {e.timestamp
                                ? new Date(e.timestamp).toLocaleString()
                                : "—"}
                        </td>
                    </tr>
                ))}
            </tbody>
        </table>
    );
}
