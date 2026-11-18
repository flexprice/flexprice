import type {
  Request as ExpressRequest,
  Response as ExpressResponse,
} from "express";

// Replaces the generated install page: GET / stays a 200 for the load balancer health checks and shows nothing.
export function landingPage(_req: Request): Response {
  return new Response(null, { status: 200 });
}

export function landingPageExpress(_req: ExpressRequest, res: ExpressResponse) {
  res.status(200).end();
}

export function landingPageHTML(_origin: string): string {
  return "";
}
