import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
    title: "Flexprice Next.js Cost Dashboard Example",
    description:
        "A minimal Next.js app showing how to use @flexprice/sdk to ingest events and render a cost dashboard",
    metadataBase: new URL("https://example.local"),
    openGraph: {
        title: "Flexprice Next.js Cost Dashboard Example",
        description: "Ingest usage and view costs using @flexprice/sdk",
        type: "website",
    },
    twitter: {
        card: "summary_large_image",
        title: "Flexprice Next.js Cost Dashboard Example",
        description: "Ingest usage and view costs using @flexprice/sdk",
    },
};

export default function RootLayout({
    children,
}: {
    children: React.ReactNode;
}) {
    return (
        <html lang="en">
            <body>
                <header className="header">
                    <div className="container">
                        <h1>Flexprice • Next.js Cost Dashboard</h1>
                    </div>
                </header>
                <main className="container">{children}</main>
                <footer className="footer">
                    <div className="container">
                        <a
                            href="https://github.com/flexprice/flexprice"
                            target="_blank"
                            rel="noreferrer"
                        >
                            Flexprice
                        </a>
                        <span>•</span>
                        <a
                            href="https://www.npmjs.com/package/@flexprice/sdk"
                            target="_blank"
                            rel="noreferrer"
                        >
                            @flexprice/sdk
                        </a>
                    </div>
                </footer>
            </body>
        </html>
    );
}
