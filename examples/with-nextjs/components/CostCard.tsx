type Props = {
    title: string;
    value: string | number;
    subtitle?: string;
    loading?: boolean;
};

export default function CostCard({ title, value, subtitle, loading }: Props) {
    return (
        <div className="card" role="status" aria-busy={loading}>
            <h3>{title}</h3>
            <div className="value">{loading ? "…" : value}</div>
            {subtitle ? <div className="subtitle">{subtitle}</div> : null}
        </div>
    );
}
