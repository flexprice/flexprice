# Cost Sheet PRD

### _Base Level Implementation of Cost Sheet Engine_

## 1. Overview

The Cost Sheet Engine enables developers to track their input costs and calculate margins by integrating with the existing billing system. It follows the same patterns as the revenue calculation workflow but focuses on cost tracking and margin analysis.

## 2. Core Architecture

### 2.1 Database Schema

```sql
CREATE TABLE public.cost_sheet_items (
    id              VARCHAR(50) DEFAULT extensions.uuid_generate_v4() PRIMARY KEY,
    meter_id        VARCHAR(50) NOT NULL,
    price_id        VARCHAR(50) NOT NULL,
    TenantID      string
	EnvironmentID string
    status          VARCHAR(20) DEFAULT 'published' NOT NULL,
    FOREIGN KEY (meter_id)      REFERENCES meters(id),
    FOREIGN KEY (price_id)      REFERENCES prices(id),
    FOREIGN KEY (tenant_id)     REFERENCES tenants(id),
    FOREIGN KEY (environment_id) REFERENCES environments(id)
);
```

### 2.2 System Components

1. **Billing Service Extension**
   - Houses the main cost calculation logic
   - Parallels existing revenue calculation
   - Integrates with existing usage tracking

2. **Price Service Extension**
   - Parallel cost calculation methods
   - Reuses existing price models (flat, package, tiered)

3. **Usage Integration**
   - Leverages existing BulkGetUsageByMeter
   - Same usage tracking infrastructure

## 3. Workflow

### 3.1 Cost Calculation Flow


<svg aria-roledescription="flowchart-v2" role="graphics-document document" viewBox="-8 -8 593.796875 467.60003662109375" style="max-width: 593.796875px;" xmlns="http://www.w3.org/2000/svg" width="100%" id="mermaid-svg-1749529562038-tp0jz51sh"><style>#mermaid-svg-1749529562038-tp0jz51sh{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh .error-icon{fill:#bf616a;}#mermaid-svg-1749529562038-tp0jz51sh .error-text{fill:#bf616a;stroke:#bf616a;}#mermaid-svg-1749529562038-tp0jz51sh .edge-thickness-normal{stroke-width:2px;}#mermaid-svg-1749529562038-tp0jz51sh .edge-thickness-thick{stroke-width:3.5px;}#mermaid-svg-1749529562038-tp0jz51sh .edge-pattern-solid{stroke-dasharray:0;}#mermaid-svg-1749529562038-tp0jz51sh .edge-pattern-dashed{stroke-dasharray:3;}#mermaid-svg-1749529562038-tp0jz51sh .edge-pattern-dotted{stroke-dasharray:2;}#mermaid-svg-1749529562038-tp0jz51sh .marker{fill:rgba(204, 204, 204, 0.87);stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh .marker.cross{stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh svg{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;}#mermaid-svg-1749529562038-tp0jz51sh .label{font-family:"trebuchet ms",verdana,arial,sans-serif;color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh .cluster-label text{fill:#ffffff;}#mermaid-svg-1749529562038-tp0jz51sh .cluster-label span,#mermaid-svg-1749529562038-tp0jz51sh p{color:#ffffff;}#mermaid-svg-1749529562038-tp0jz51sh .label text,#mermaid-svg-1749529562038-tp0jz51sh span,#mermaid-svg-1749529562038-tp0jz51sh p{fill:rgba(204, 204, 204, 0.87);color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh .node rect,#mermaid-svg-1749529562038-tp0jz51sh .node circle,#mermaid-svg-1749529562038-tp0jz51sh .node ellipse,#mermaid-svg-1749529562038-tp0jz51sh .node polygon,#mermaid-svg-1749529562038-tp0jz51sh .node path{fill:#1a1a1a;stroke:#2a2a2a;stroke-width:1px;}#mermaid-svg-1749529562038-tp0jz51sh .flowchart-label text{text-anchor:middle;}#mermaid-svg-1749529562038-tp0jz51sh .node .label{text-align:center;}#mermaid-svg-1749529562038-tp0jz51sh .node.clickable{cursor:pointer;}#mermaid-svg-1749529562038-tp0jz51sh .arrowheadPath{fill:#e5e5e5;}#mermaid-svg-1749529562038-tp0jz51sh .edgePath .path{stroke:rgba(204, 204, 204, 0.87);stroke-width:2.0px;}#mermaid-svg-1749529562038-tp0jz51sh .flowchart-link{stroke:rgba(204, 204, 204, 0.87);fill:none;}#mermaid-svg-1749529562038-tp0jz51sh .edgeLabel{background-color:#1a1a1a99;text-align:center;}#mermaid-svg-1749529562038-tp0jz51sh .edgeLabel rect{opacity:0.5;background-color:#1a1a1a99;fill:#1a1a1a99;}#mermaid-svg-1749529562038-tp0jz51sh .labelBkg{background-color:rgba(26, 26, 26, 0.5);}#mermaid-svg-1749529562038-tp0jz51sh .cluster rect{fill:rgba(64, 64, 64, 0.47);stroke:#30373a;stroke-width:1px;}#mermaid-svg-1749529562038-tp0jz51sh .cluster text{fill:#ffffff;}#mermaid-svg-1749529562038-tp0jz51sh .cluster span,#mermaid-svg-1749529562038-tp0jz51sh p{color:#ffffff;}#mermaid-svg-1749529562038-tp0jz51sh div.mermaidTooltip{position:absolute;text-align:center;max-width:200px;padding:2px;font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:12px;background:#88c0d0;border:1px solid #30373a;border-radius:2px;pointer-events:none;z-index:100;}#mermaid-svg-1749529562038-tp0jz51sh .flowchartTitleText{text-anchor:middle;font-size:18px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh :root{--mermaid-font-family:"trebuchet ms",verdana,arial,sans-serif;}</style><g><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="6" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 0 L 10 5 L 0 10 z"/></marker><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="4.5" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointStart"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 5 L 10 10 L 10 0 z"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="11" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-circleEnd"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="-1" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-circleStart"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="12" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-crossEnd"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="-1" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-crossStart"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><g class="root"><g class="clusters"/><g class="edgePaths"><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-A LE-B" id="L-A-B-0" d="M198.067,33.6L198.067,37.767C198.067,41.933,198.067,50.267,198.067,57.717C198.067,65.167,198.067,71.733,198.067,75.017L198.067,78.3"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-B LE-C1" id="L-B-C1-0" d="M248.18,117.2L260.608,121.367C273.037,125.533,297.894,133.867,310.323,141.317C322.752,148.767,322.752,155.333,322.752,158.617L322.752,161.9"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-B LE-C2" id="L-B-C2-0" d="M156.139,117.2L145.74,121.367C135.341,125.533,114.543,133.867,104.144,141.317C93.745,148.767,93.745,155.333,93.745,158.617L93.745,161.9"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C2 LE-D" id="L-C2-D-0" d="M93.745,200.8L93.745,204.967C93.745,209.133,93.745,217.467,93.745,224.917C93.745,232.367,93.745,238.933,93.745,242.217L93.745,245.5"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C1 LE-E" id="L-C1-E-0" d="M322.752,200.8L322.752,204.967C322.752,209.133,322.752,217.467,321.109,225.006C319.466,232.545,316.18,239.29,314.537,242.663L312.894,246.035"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C2 LE-E" id="L-C2-E-0" d="M169.947,200.8L188.846,204.967C207.745,209.133,245.543,217.467,265.975,224.996C286.406,232.526,289.471,239.251,291.003,242.614L292.536,245.977"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-E LE-F" id="L-E-F-0" d="M302.389,284.4L302.389,288.567C302.389,292.733,302.389,301.067,302.389,308.517C302.389,315.967,302.389,322.533,302.389,325.817L302.389,329.1"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-G1" id="L-F-G1-0" d="M245.633,362.571L220.318,367.642C195.004,372.714,144.374,382.857,119.06,391.212C93.745,399.567,93.745,406.133,93.745,409.417L93.745,412.7"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-G2" id="L-F-G2-0" d="M302.389,368L302.389,372.167C302.389,376.333,302.389,384.667,302.389,392.117C302.389,399.567,302.389,406.133,302.389,409.417L302.389,412.7"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-G3" id="L-F-G3-0" d="M359.145,362.929L383.397,367.941C407.648,372.953,456.151,382.976,480.402,391.272C504.653,399.567,504.653,406.133,504.653,409.417L504.653,412.7"/></g><g class="edgeLabels"><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g></g><g class="nodes"><g transform="translate(198.06719207763672, 16.80000114440918)" id="flowchart-A-73" class="node default default flowchart-label"><rect height="33.60000038146973" width="115.87501525878906" y="-16.800000190734863" x="-57.93750762939453" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-50.43750762939453, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="100.87501525878906"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Billing Service</span></div></foreignObject></g></g><g transform="translate(198.06719207763672, 100.40000343322754)" id="flowchart-B-74" class="node default default flowchart-label"><rect height="33.60000038146973" width="157.57501220703125" y="-16.800000190734863" x="-78.78750610351562" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-71.28750610351562, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="142.57501220703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateAllCharges</span></div></foreignObject></g></g><g transform="translate(322.7515640258789, 184.0000057220459)" id="flowchart-C1-76" class="node default default flowchart-label"><rect height="33.60000038146973" width="177.30938720703125" y="-16.800000190734863" x="-88.65469360351562" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-81.15469360351562, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="162.30938720703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateFixedCharges</span></div></foreignObject></g></g><g transform="translate(93.74531555175781, 184.0000057220459)" id="flowchart-C2-78" class="node default default flowchart-label"><rect height="33.60000038146973" width="180.703125" y="-16.800000190734863" x="-90.3515625" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-82.8515625, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="165.703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateUsageCharges</span></div></foreignObject></g></g><g transform="translate(93.74531555175781, 267.60000801086426)" id="flowchart-D-80" class="node default default flowchart-label"><rect height="33.60000038146973" width="187.49063110351562" y="-16.800000190734863" x="-93.74531555175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-86.24531555175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="172.49063110351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">GetUsageBySubscription</span></div></foreignObject></g></g><g transform="translate(302.3890686035156, 267.60000801086426)" id="flowchart-E-83" class="node default default flowchart-label"><rect height="33.60000038146973" width="107.09062957763672" y="-16.800000190734863" x="-53.54531478881836" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-46.04531478881836, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="92.09062957763672"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Price Service</span></div></foreignObject></g></g><g transform="translate(302.3890686035156, 351.2000102996826)" id="flowchart-F-85" class="node default default flowchart-label"><rect height="33.60000038146973" width="113.51250457763672" y="-16.800000190734863" x="-56.75625228881836" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-49.25625228881836, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="98.51250457763672"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateCost</span></div></foreignObject></g></g><g transform="translate(93.74531555175781, 434.800012588501)" id="flowchart-G1-87" class="node default default flowchart-label"><rect height="33.60000038146973" width="159.046875" y="-16.800000190734863" x="-79.5234375" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-72.0234375, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="144.046875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Flat Fee Calculation</span></div></foreignObject></g></g><g transform="translate(302.3890686035156, 434.800012588501)" id="flowchart-G2-89" class="node default default flowchart-label"><rect height="33.60000038146973" width="158.24063110351562" y="-16.800000190734863" x="-79.12031555175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-71.62031555175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="143.24063110351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Package Calculation</span></div></foreignObject></g></g><g transform="translate(504.65313720703125, 434.800012588501)" id="flowchart-G3-91" class="node default default flowchart-label"><rect height="33.60000038146973" width="146.28750610351562" y="-16.800000190734863" x="-73.14375305175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-65.64375305175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="131.28750610351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Tiered Calculation</span></div></foreignObject></g></g></g></g></g></svg>


### 3.2 Core Functions

1. **GetInputCostForMargin**
   - Fetches all published cost items
   - Gets usage data via BulkGetUsageByMeter
   - Calculates costs using price service
   - Returns aggregated input costs

2. **CalculateMargin**
   - Input: TotalInputCost, TotalRevenue
   - Calculates: (TotalRevenue - TotalCost) / TotalCost
   - Returns margin percentage

## 4. Service Interfaces

```go
type BillingService interface {
    // Existing methods...
    
    // New methods for cost sheet
    GetInputCostForMargin(ctx context.Context, req *dto.GetInputCostRequest) (*dto.GetInputCostResponse, error)
    CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal
}

type PriceService interface {
    // Existing methods...
    
    // New methods for cost calculation
    CalculateCostSheetPrice(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal
}
```

## 5. API Endpoints

### 5.1 Cost Sheet Management

```http
POST /cost_sheets/items
{
    "meter_id": "string",
    "price_id": "string"
}
```

```http
GET /cost_sheets/items
```

```http
GET /cost_sheets/items/{id}
```

```http
PUT /cost_sheets/items/{id}
```

```http
DELETE /cost_sheets/items/{id}
```

### 5.2 Cost & Margin Endpoints

```http
GET /subscriptions/{id}/cost_sheet
Response:
{
    "input_costs": {
        "total": "decimal",
        "items": [
            {
                "meter_id": "string",
                "usage": "decimal",
                "cost": "decimal"
            }
        ]
    },
    "revenue": "decimal",
    "margin": "decimal"
}
```

## 6. Implementation Strategy

### Phase 1: Core Infrastructure
1. Add cost_sheet_items table
2. Implement basic cost calculation
3. Extend BillingService with cost methods
4. Extend PriceService with cost calculations

### Phase 2: Service Integration
1. Connect with existing usage tracking
2. Implement margin calculations
3. Add cost aggregation logic

### Phase 3: API Layer
1. Add cost sheet endpoints
2. Implement input validation
3. Add error handling

## 8. Integration Points

1. **With Existing Billing System**
   - Reuses BulkGetUsageByMeter for usage tracking
   - Parallels existing price calculation methods
   - Integrates with current billing workflow

2. **With Price Service**
   - Extends price calculations for cost
   - Reuses existing price models (flat, package, tiered)
   - Maintains co

### Visuals

### Parallel Cost Sheet Workflow

<svg aria-roledescription="flowchart-v2" role="graphics-document document" viewBox="-7.999996185302734 -8 527.9390563964844 802" style="max-width: 527.9390563964844px;" xmlns="http://www.w3.org/2000/svg" width="100%" id="mermaid-svg-1749529564747-3znm8i0st"><style>#mermaid-svg-1749529564747-3znm8i0st{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529564747-3znm8i0st .error-icon{fill:#bf616a;}#mermaid-svg-1749529564747-3znm8i0st .error-text{fill:#bf616a;stroke:#bf616a;}#mermaid-svg-1749529564747-3znm8i0st .edge-thickness-normal{stroke-width:2px;}#mermaid-svg-1749529564747-3znm8i0st .edge-thickness-thick{stroke-width:3.5px;}#mermaid-svg-1749529564747-3znm8i0st .edge-pattern-solid{stroke-dasharray:0;}#mermaid-svg-1749529564747-3znm8i0st .edge-pattern-dashed{stroke-dasharray:3;}#mermaid-svg-1749529564747-3znm8i0st .edge-pattern-dotted{stroke-dasharray:2;}#mermaid-svg-1749529564747-3znm8i0st .marker{fill:rgba(204, 204, 204, 0.87);stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529564747-3znm8i0st .marker.cross{stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529564747-3znm8i0st svg{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;}#mermaid-svg-1749529564747-3znm8i0st .label{font-family:"trebuchet ms",verdana,arial,sans-serif;color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529564747-3znm8i0st .cluster-label text{fill:#ffffff;}#mermaid-svg-1749529564747-3znm8i0st .cluster-label span,#mermaid-svg-1749529564747-3znm8i0st p{color:#ffffff;}#mermaid-svg-1749529564747-3znm8i0st .label text,#mermaid-svg-1749529564747-3znm8i0st span,#mermaid-svg-1749529564747-3znm8i0st p{fill:rgba(204, 204, 204, 0.87);color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529564747-3znm8i0st .node rect,#mermaid-svg-1749529564747-3znm8i0st .node circle,#mermaid-svg-1749529564747-3znm8i0st .node ellipse,#mermaid-svg-1749529564747-3znm8i0st .node polygon,#mermaid-svg-1749529564747-3znm8i0st .node path{fill:#1a1a1a;stroke:#2a2a2a;stroke-width:1px;}#mermaid-svg-1749529564747-3znm8i0st .flowchart-label text{text-anchor:middle;}#mermaid-svg-1749529564747-3znm8i0st .node .label{text-align:center;}#mermaid-svg-1749529564747-3znm8i0st .node.clickable{cursor:pointer;}#mermaid-svg-1749529564747-3znm8i0st .arrowheadPath{fill:#e5e5e5;}#mermaid-svg-1749529564747-3znm8i0st .edgePath .path{stroke:rgba(204, 204, 204, 0.87);stroke-width:2.0px;}#mermaid-svg-1749529564747-3znm8i0st .flowchart-link{stroke:rgba(204, 204, 204, 0.87);fill:none;}#mermaid-svg-1749529564747-3znm8i0st .edgeLabel{background-color:#1a1a1a99;text-align:center;}#mermaid-svg-1749529564747-3znm8i0st .edgeLabel rect{opacity:0.5;background-color:#1a1a1a99;fill:#1a1a1a99;}#mermaid-svg-1749529564747-3znm8i0st .labelBkg{background-color:rgba(26, 26, 26, 0.5);}#mermaid-svg-1749529564747-3znm8i0st .cluster rect{fill:rgba(64, 64, 64, 0.47);stroke:#30373a;stroke-width:1px;}#mermaid-svg-1749529564747-3znm8i0st .cluster text{fill:#ffffff;}#mermaid-svg-1749529564747-3znm8i0st .cluster span,#mermaid-svg-1749529564747-3znm8i0st p{color:#ffffff;}#mermaid-svg-1749529564747-3znm8i0st div.mermaidTooltip{position:absolute;text-align:center;max-width:200px;padding:2px;font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:12px;background:#88c0d0;border:1px solid #30373a;border-radius:2px;pointer-events:none;z-index:100;}#mermaid-svg-1749529564747-3znm8i0st .flowchartTitleText{text-anchor:middle;font-size:18px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529564747-3znm8i0st :root{--mermaid-font-family:"trebuchet ms",verdana,arial,sans-serif;}</style><g><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="6" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 0 L 10 5 L 0 10 z"/></marker><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="4.5" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529564747-3znm8i0st_flowchart-pointStart"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 5 L 10 10 L 10 0 z"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="11" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529564747-3znm8i0st_flowchart-circleEnd"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="-1" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529564747-3znm8i0st_flowchart-circleStart"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="12" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749529564747-3znm8i0st_flowchart-crossEnd"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="-1" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749529564747-3znm8i0st_flowchart-crossStart"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><g class="root"><g class="clusters"/><g class="edgePaths"><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-A LE-B" id="L-A-B-0" d="M338.739,33.6L338.739,37.767C338.739,41.933,338.739,50.267,338.739,57.717C338.739,65.167,338.739,71.733,338.739,75.017L338.739,78.3"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-B LE-C1" id="L-B-C1-0" d="M276.816,117.2L261.458,121.367C246.1,125.533,215.385,133.867,200.027,141.317C184.669,148.767,184.669,155.333,184.669,158.617L184.669,161.9"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C1 LE-D1" id="L-C1-D1-0" d="M184.669,200.8L184.669,204.967C184.669,209.133,184.669,217.467,184.669,224.917C184.669,232.367,184.669,238.933,184.669,242.217L184.669,245.5"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-D1 LE-D2" id="L-D1-D2-0" d="M184.669,284.4L184.669,288.567C184.669,292.733,184.669,301.067,184.669,308.517C184.669,315.967,184.669,322.533,184.669,325.817L184.669,329.1"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-D2 LE-E" id="L-D2-E-0" d="M184.669,368L184.669,372.167C184.669,376.333,184.669,384.667,184.669,392.117C184.669,399.567,184.669,406.133,184.669,409.417L184.669,412.7"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-E LE-F" id="L-E-F-0" d="M184.669,451.6L184.669,455.767C184.669,459.933,184.669,468.267,184.669,475.717C184.669,483.167,184.669,489.733,184.669,493.017L184.669,496.3"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-G" id="L-F-G-0" d="M176.63,535.2L174.637,539.367C172.643,543.533,168.656,551.867,166.662,559.317C164.669,566.767,164.669,573.333,164.669,576.617L164.669,579.9"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-G LE-H" id="L-G-H-0" d="M164.669,618.8L164.669,622.967C164.669,627.133,164.669,635.467,164.669,642.917C164.669,650.367,164.669,656.933,164.669,660.217L164.669,663.5"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-H LE-I1" id="L-H-I1-0" d="M120.453,702.4L109.487,706.567C98.521,710.733,76.589,719.067,65.622,726.517C54.656,733.967,54.656,740.533,54.656,743.817L54.656,747.1"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-H LE-I2" id="L-H-I2-0" d="M184.323,702.4L189.198,706.567C194.072,710.733,203.821,719.067,208.696,726.517C213.57,733.967,213.57,740.533,213.57,743.817L213.57,747.1"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-H LE-I3" id="L-H-I3-0" d="M245.629,702.4L265.708,706.567C285.787,710.733,325.946,719.067,346.025,726.517C366.105,733.967,366.105,740.533,366.105,743.817L366.105,747.1"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-B LE-J" id="L-B-J-0" d="M369.392,117.2L376.994,121.367C384.597,125.533,399.801,133.867,407.404,145C415.006,156.133,415.006,170.067,415.006,184C415.006,197.933,415.006,211.867,415.006,225.8C415.006,239.733,415.006,253.667,415.006,267.6C415.006,281.533,415.006,295.467,415.006,309.4C415.006,323.333,415.006,337.267,415.006,351.2C415.006,365.133,415.006,379.067,415.006,393C415.006,406.933,415.006,420.867,415.006,434.8C415.006,448.733,415.006,462.667,415.006,472.917C415.006,483.167,415.006,489.733,415.006,493.017L415.006,496.3"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-K" id="L-F-K-0" d="M210.638,535.2L217.078,539.367C223.519,543.533,236.401,551.867,258.504,559.984C280.608,568.101,311.934,576.003,327.597,579.953L343.26,583.904"/><path marker-end="url(#mermaid-svg-1749529564747-3znm8i0st_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-J LE-K" id="L-J-K-0" d="M415.006,535.2L415.006,539.367C415.006,543.533,415.006,551.867,415.006,559.317C415.006,566.767,415.006,573.333,415.006,576.617L415.006,579.9"/></g><g class="edgeLabels"><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g></g><g class="nodes"><g transform="translate(338.73906326293945, 16.80000114440918)" id="flowchart-A-119" class="node default default flowchart-label"><rect height="33.60000038146973" width="115.87501525878906" y="-16.800000190734863" x="-57.93750762939453" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-50.43750762939453, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="100.87501525878906"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Billing Service</span></div></foreignObject></g></g><g transform="translate(338.73906326293945, 100.40000343322754)" id="flowchart-B-120" class="node default default flowchart-label"><rect height="33.60000038146973" width="138.8531265258789" y="-16.800000190734863" x="-69.42656326293945" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-61.92656326293945, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="123.8531265258789"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateAllCosts</span></div></foreignObject></g></g><g transform="translate(184.66875076293945, 184.0000057220459)" id="flowchart-C1-122" class="node default default flowchart-label"><rect height="33.60000038146973" width="179.54063415527344" y="-16.800000190734863" x="-89.77031707763672" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-82.27031707763672, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="164.54063415527344"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">GetInputCostForMargin</span></div></foreignObject></g></g><g transform="translate(184.66875076293945, 267.60000801086426)" id="flowchart-D1-124" class="node default default flowchart-label"><rect height="33.60000038146973" width="184.43438720703125" y="-16.800000190734863" x="-92.21719360351562" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-84.71719360351562, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="169.43438720703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Fetch cost_sheet_items</span></div></foreignObject></g></g><g transform="translate(184.66875076293945, 351.2000102996826)" id="flowchart-D2-126" class="node default default flowchart-label"><rect height="33.60000038146973" width="205.36875915527344" y="-16.800000190734863" x="-102.68437957763672" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-95.18437957763672, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="190.36875915527344"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Filter by status='published'</span></div></foreignObject></g></g><g transform="translate(184.66875076293945, 434.800012588501)" id="flowchart-E-128" class="node default default flowchart-label"><rect height="33.60000038146973" width="171.7687530517578" y="-16.800000190734863" x="-85.8843765258789" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-78.3843765258789, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="156.7687530517578"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">BulkGetUsageByMeter</span></div></foreignObject></g></g><g transform="translate(184.66875076293945, 518.4000148773193)" id="flowchart-F-130" class="node default default flowchart-label"><rect height="33.60000038146973" width="166.80938720703125" y="-16.800000190734863" x="-83.40469360351562" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-75.90469360351562, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="151.80938720703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Calculate Input Costs</span></div></foreignObject></g></g><g transform="translate(164.66875076293945, 602.0000171661377)" id="flowchart-G-132" class="node default default flowchart-label"><rect height="33.60000038146973" width="107.09062957763672" y="-16.800000190734863" x="-53.54531478881836" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-46.04531478881836, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="92.09062957763672"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Price Service</span></div></foreignObject></g></g><g transform="translate(164.66875076293945, 685.600019454956)" id="flowchart-H-134" class="node default default flowchart-label"><rect height="33.60000038146973" width="189.328125" y="-16.800000190734863" x="-94.6640625" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-87.1640625, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="174.328125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateCostSheetPrice</span></div></foreignObject></g></g><g transform="translate(54.65625, 769.2000217437744)" id="flowchart-I1-136" class="node default default flowchart-label"><rect height="33.60000038146973" width="109.3125" y="-16.800000190734863" x="-54.65625" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-47.15625, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="94.3125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Flat Fee Cost</span></div></foreignObject></g></g><g transform="translate(213.5703125, 769.2000217437744)" id="flowchart-I2-138" class="node default default flowchart-label"><rect height="33.60000038146973" width="108.515625" y="-16.800000190734863" x="-54.2578125" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-46.7578125, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="93.515625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Package Cost</span></div></foreignObject></g></g><g transform="translate(366.1046905517578, 769.2000217437744)" id="flowchart-I3-140" class="node default default flowchart-label"><rect height="33.60000038146973" width="96.55313110351562" y="-16.800000190734863" x="-48.27656555175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-40.77656555175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="81.55313110351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Tiered Cost</span></div></foreignObject></g></g><g transform="translate(415.00625228881836, 518.4000148773193)" id="flowchart-J-142" class="node default default flowchart-label"><rect height="33.60000038146973" width="193.86563110351562" y="-16.800000190734863" x="-96.93281555175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-89.43281555175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="178.86563110351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Get Revenue from Billing</span></div></foreignObject></g></g><g transform="translate(415.00625228881836, 602.0000171661377)" id="flowchart-K-145" class="node default default flowchart-label"><rect height="33.60000038146973" width="134.66250610351562" y="-16.800000190734863" x="-67.33125305175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-59.83125305175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="119.66250610351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Calculate Margin</span></div></foreignObject></g></g></g></g></g></svg>


### Overall cost sheet workflow
<svg aria-roledescription="flowchart-v2" role="graphics-document document" viewBox="-7.999992370605469 -8 590.253173828125 384.0000305175781" style="max-width: 590.253173828125px;" xmlns="http://www.w3.org/2000/svg" width="100%" id="mermaid-svg-1749529164043-0ai89arh0"><style>#mermaid-svg-1749529164043-0ai89arh0{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529164043-0ai89arh0 .error-icon{fill:#bf616a;}#mermaid-svg-1749529164043-0ai89arh0 .error-text{fill:#bf616a;stroke:#bf616a;}#mermaid-svg-1749529164043-0ai89arh0 .edge-thickness-normal{stroke-width:2px;}#mermaid-svg-1749529164043-0ai89arh0 .edge-thickness-thick{stroke-width:3.5px;}#mermaid-svg-1749529164043-0ai89arh0 .edge-pattern-solid{stroke-dasharray:0;}#mermaid-svg-1749529164043-0ai89arh0 .edge-pattern-dashed{stroke-dasharray:3;}#mermaid-svg-1749529164043-0ai89arh0 .edge-pattern-dotted{stroke-dasharray:2;}#mermaid-svg-1749529164043-0ai89arh0 .marker{fill:rgba(204, 204, 204, 0.87);stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529164043-0ai89arh0 .marker.cross{stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529164043-0ai89arh0 svg{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;}#mermaid-svg-1749529164043-0ai89arh0 .label{font-family:"trebuchet ms",verdana,arial,sans-serif;color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529164043-0ai89arh0 .cluster-label text{fill:#ffffff;}#mermaid-svg-1749529164043-0ai89arh0 .cluster-label span,#mermaid-svg-1749529164043-0ai89arh0 p{color:#ffffff;}#mermaid-svg-1749529164043-0ai89arh0 .label text,#mermaid-svg-1749529164043-0ai89arh0 span,#mermaid-svg-1749529164043-0ai89arh0 p{fill:rgba(204, 204, 204, 0.87);color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529164043-0ai89arh0 .node rect,#mermaid-svg-1749529164043-0ai89arh0 .node circle,#mermaid-svg-1749529164043-0ai89arh0 .node ellipse,#mermaid-svg-1749529164043-0ai89arh0 .node polygon,#mermaid-svg-1749529164043-0ai89arh0 .node path{fill:#1a1a1a;stroke:#2a2a2a;stroke-width:1px;}#mermaid-svg-1749529164043-0ai89arh0 .flowchart-label text{text-anchor:middle;}#mermaid-svg-1749529164043-0ai89arh0 .node .label{text-align:center;}#mermaid-svg-1749529164043-0ai89arh0 .node.clickable{cursor:pointer;}#mermaid-svg-1749529164043-0ai89arh0 .arrowheadPath{fill:#e5e5e5;}#mermaid-svg-1749529164043-0ai89arh0 .edgePath .path{stroke:rgba(204, 204, 204, 0.87);stroke-width:2.0px;}#mermaid-svg-1749529164043-0ai89arh0 .flowchart-link{stroke:rgba(204, 204, 204, 0.87);fill:none;}#mermaid-svg-1749529164043-0ai89arh0 .edgeLabel{background-color:#1a1a1a99;text-align:center;}#mermaid-svg-1749529164043-0ai89arh0 .edgeLabel rect{opacity:0.5;background-color:#1a1a1a99;fill:#1a1a1a99;}#mermaid-svg-1749529164043-0ai89arh0 .labelBkg{background-color:rgba(26, 26, 26, 0.5);}#mermaid-svg-1749529164043-0ai89arh0 .cluster rect{fill:rgba(64, 64, 64, 0.47);stroke:#30373a;stroke-width:1px;}#mermaid-svg-1749529164043-0ai89arh0 .cluster text{fill:#ffffff;}#mermaid-svg-1749529164043-0ai89arh0 .cluster span,#mermaid-svg-1749529164043-0ai89arh0 p{color:#ffffff;}#mermaid-svg-1749529164043-0ai89arh0 div.mermaidTooltip{position:absolute;text-align:center;max-width:200px;padding:2px;font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:12px;background:#88c0d0;border:1px solid #30373a;border-radius:2px;pointer-events:none;z-index:100;}#mermaid-svg-1749529164043-0ai89arh0 .flowchartTitleText{text-anchor:middle;font-size:18px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529164043-0ai89arh0 :root{--mermaid-font-family:"trebuchet ms",verdana,arial,sans-serif;}</style><g><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="6" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 0 L 10 5 L 0 10 z"/></marker><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="4.5" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529164043-0ai89arh0_flowchart-pointStart"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 5 L 10 10 L 10 0 z"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="11" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529164043-0ai89arh0_flowchart-circleEnd"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="-1" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529164043-0ai89arh0_flowchart-circleStart"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="12" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749529164043-0ai89arh0_flowchart-crossEnd"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="-1" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749529164043-0ai89arh0_flowchart-crossStart"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><g class="root"><g class="clusters"/><g class="edgePaths"><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-A LE-B" id="L-A-B-0" d="M92.217,33.6L92.217,37.767C92.217,41.933,92.217,50.267,92.217,57.717C92.217,65.167,92.217,71.733,92.217,75.017L92.217,78.3"/><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-B LE-C" id="L-B-C-0" d="M92.217,117.2L92.217,121.367C92.217,125.533,92.217,133.867,92.217,141.317C92.217,148.767,92.217,155.333,92.217,158.617L92.217,161.9"/><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C LE-D" id="L-C-D-0" d="M92.217,200.8L92.217,204.967C92.217,209.133,92.217,217.467,92.217,224.917C92.217,232.367,92.217,238.933,92.217,242.217L92.217,245.5"/><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-D LE-E" id="L-D-E-0" d="M92.217,284.4L92.217,288.567C92.217,292.733,92.217,301.067,92.217,308.517C92.217,315.967,92.217,322.533,92.217,325.817L92.217,329.1"/><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-G" id="L-F-G-0" d="M363.174,33.6L353.459,37.767C343.745,41.933,324.316,50.267,314.602,57.717C304.888,65.167,304.888,71.733,304.888,75.017L304.888,78.3"/><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-H" id="L-F-H-0" d="M441.511,33.6L451.225,37.767C460.939,41.933,480.368,50.267,490.083,57.717C499.797,65.167,499.797,71.733,499.797,75.017L499.797,78.3"/><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-G LE-I" id="L-G-I-0" d="M304.888,117.2L304.888,121.367C304.888,125.533,304.888,133.867,313.79,141.852C322.693,149.837,340.498,157.474,349.4,161.292L358.303,165.111"/><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-H LE-I" id="L-H-I-0" d="M499.797,117.2L499.797,121.367C499.797,125.533,499.797,133.867,490.894,141.852C481.992,149.837,464.187,157.474,455.284,161.292L446.381,165.111"/><path marker-end="url(#mermaid-svg-1749529164043-0ai89arh0_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-I LE-J" id="L-I-J-0" d="M402.342,200.8L402.342,204.967C402.342,209.133,402.342,217.467,402.342,224.917C402.342,232.367,402.342,238.933,402.342,242.217L402.342,245.5"/></g><g class="edgeLabels"><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g></g><g class="nodes"><g transform="translate(92.2171859741211, 16.80000114440918)" id="flowchart-A-36" class="node default default flowchart-label"><rect height="33.60000038146973" width="179.54063415527344" y="-16.800000190734863" x="-89.77031707763672" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-82.27031707763672, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="164.54063415527344"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">GetInputCostForMargin</span></div></foreignObject></g></g><g transform="translate(92.2171859741211, 100.40000343322754)" id="flowchart-B-37" class="node default default flowchart-label"><rect height="33.60000038146973" width="184.43438720703125" y="-16.800000190734863" x="-92.21719360351562" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-84.71719360351562, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="169.43438720703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Fetch cost_sheet_items</span></div></foreignObject></g></g><g transform="translate(92.2171859741211, 184.0000057220459)" id="flowchart-C-39" class="node default default flowchart-label"><rect height="33.60000038146973" width="171.7687530517578" y="-16.800000190734863" x="-85.8843765258789" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-78.3843765258789, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="156.7687530517578"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">BulkGetUsageByMeter</span></div></foreignObject></g></g><g transform="translate(92.2171859741211, 267.60000801086426)" id="flowchart-D-41" class="node default default flowchart-label"><rect height="33.60000038146973" width="150.69375610351562" y="-16.800000190734863" x="-75.34687805175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-67.84687805175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="135.69375610351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateInputCost</span></div></foreignObject></g></g><g transform="translate(92.2171859741211, 351.2000102996826)" id="flowchart-E-43" class="node default default flowchart-label"><rect height="33.60000038146973" width="170.4562530517578" y="-16.800000190734863" x="-85.2281265258789" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-77.7281265258789, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="155.4562530517578"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Return TotalInputCost</span></div></foreignObject></g></g><g transform="translate(402.3421974182129, 16.80000114440918)" id="flowchart-F-44" class="node default default flowchart-label"><rect height="33.60000038146973" width="129.84376525878906" y="-16.800000190734863" x="-64.92188262939453" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-57.42188262939453, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="114.84376525878906"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateMargin</span></div></foreignObject></g></g><g transform="translate(304.8875045776367, 100.40000343322754)" id="flowchart-G-45" class="node default default flowchart-label"><rect height="33.60000038146973" width="140.90626525878906" y="-16.800000190734863" x="-70.45313262939453" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-62.95313262939453, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="125.90626525878906"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Get TotalRevenue</span></div></foreignObject></g></g><g transform="translate(499.79689025878906, 100.40000343322754)" id="flowchart-H-47" class="node default default flowchart-label"><rect height="33.60000038146973" width="148.91250610351562" y="-16.800000190734863" x="-74.45625305175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-66.95625305175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="133.91250610351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Get TotalInputCost</span></div></foreignObject></g></g><g transform="translate(402.3421974182129, 184.0000057220459)" id="flowchart-I-49" class="node default default flowchart-label"><rect height="33.60000038146973" width="134.66250610351562" y="-16.800000190734863" x="-67.33125305175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-59.83125305175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="119.66250610351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Calculate Margin</span></div></foreignObject></g></g><g transform="translate(402.3421974182129, 267.60000801086426)" id="flowchart-J-53" class="node default default flowchart-label"><rect height="33.60000038146973" width="114.55313110351562" y="-16.800000190734863" x="-57.27656555175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-49.77656555175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="99.55313110351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Return Margin</span></div></foreignObject></g></g></g></g></g></svg>






























### Phase 1 FLOW

Files in Consideration:

flexprice/migrations/postgres/cost_sheet.sql

flexprice/internal/types/cost_sheet.go

flexprice/internal/domain/costsheet/model.go
flexprice/internal/domain/costsheet/repository.go

flexprice/internal/repository/ent/cost_sheet.go

flexprice/internal/service/cost_sheet.go
flexprice/internal/service/price.go



## Interaction flow between these files:


1. **Database Schema (cost_sheet.sql)**
```sql
Table: cost_sheet_items
Columns:
- id: VARCHAR(50) PRIMARY KEY
  Purpose: Unique identifier for cost sheet items
  Default: UUID v4

- meter_id: VARCHAR(50) NOT NULL
  Purpose: References the usage meter
  Constraint: Foreign key to meters table

- price_id: VARCHAR(50) NOT NULL
  Purpose: References the pricing configuration
  Constraint: Foreign key to prices table

- status: VARCHAR(20) NOT NULL
  Purpose: Item current state
  Default: 'published'

- tenant_id: VARCHAR(50) NOT NULL
  Purpose: Organization identifier
  Constraint: Foreign key to tenants table

- environment_id: VARCHAR(50) NOT NULL
  Purpose: Environment context
  Constraint: Foreign key to environments table
```

2. **Domain Model (model.go)**
```go
// CostSheetItem Structure
type CostSheetItem struct {
    ID            string    // Unique identifier
    MeterID       string    // Associated meter
    PriceID       string    // Associated price config
    Status        string    // Current state
    TenantID      string    // Owner organization
    EnvironmentID string    // Environment context
    CreatedAt     time.Time // Creation timestamp
    UpdatedAt     time.Time // Last update timestamp
}

// NewCostSheetItem Function
// Purpose: Creates new cost sheet item
// Parameters:
- meterID: string - Meter identifier
- priceID: string - Price configuration identifier
- tenantID: string - Organization identifier
- environmentID: string - Environment identifier
// Returns: *CostSheetItem with default status "published"

// ToDTO Function
// Purpose: Converts domain model to DTO
// Returns: *types.CostSheetItem for API responses

// FromDTO Function
// Purpose: Creates domain model from DTO
// Parameters: dto *types.CostSheetItem
// Returns: *CostSheetItem for internal use
```

3. **Repository Interface (repository.go)**
```go
type Repository interface {
    // Create
    // Purpose: Persists new cost sheet item
    // Parameters:
    // - ctx: context.Context - Operation context
    // - item: *CostSheetItem - Item to create
    // Returns: error if creation fails

    // Get
    // Purpose: Retrieves single item by ID
    // Parameters:
    // - ctx: context.Context
    // - id: string - Item identifier
    // Returns: (*CostSheetItem, error)

    // List
    // Purpose: Retrieves multiple items by filter
    // Parameters:
    // - ctx: context.Context
    // - filter: *types.CostSheetFilter - Query criteria
    // Returns: ([]*CostSheetItem, error)

    // Count
    // Purpose: Counts items matching filter
    // Parameters:
    // - ctx: context.Context
    // - filter: *types.CostSheetFilter
    // Returns: (int, error)

    // Update
    // Purpose: Modifies existing item
    // Parameters:
    // - ctx: context.Context
    // - item: *CostSheetItem - Modified item
    // Returns: error if update fails

    // Delete
    // Purpose: Removes item by ID
    // Parameters:
    // - ctx: context.Context
    // - id: string - Item identifier
    // Returns: error if deletion fails
}
```

4. **Repository Implementation (ent/cost_sheet.go)**
```go
type repository struct {
    db *sql.DB
}

// Implementation details for each interface method:

// Create
// Logic:
1. Prepares INSERT SQL statement
2. Executes with item fields as parameters
3. Returns error if insertion fails

// Get
// Logic:
1. Executes SELECT query with ID
2. Scans result into CostSheetItem struct
3. Returns error if not found

// List
// Logic:
1. Builds SELECT query with filter conditions
2. Executes query with pagination
3. Scans results into CostSheetItem slice

// Count
// Logic:
1. Executes COUNT query with filter
2. Returns total matching records

// Update
// Logic:
1. Executes UPDATE statement
2. Verifies rows affected
3. Returns error if no update occurred

// Delete
// Logic:
1. Executes DELETE statement
2. Verifies rows affected
3. Returns error if no deletion occurred
```

5. **Service Layer (service/cost_sheet.go)**
```go
type costSheetService struct {
    ServiceParams
    costSheetRepo costsheet.Repository
    eventService  EventService
    priceService  PriceService
}

// GetInputCostForMargin
// Purpose: Calculates input costs for margin
// Parameters:
// - ctx: context.Context
// - req: *types.GetInputCostRequest
// Logic:
1. Fetches published cost sheet items
2. Creates usage requests for each meter
3. Gets bulk usage data
4. Calculates costs using price service
5. Aggregates total and item costs
// Returns: (*types.GetInputCostResponse, error)

// CalculateMargin
// Purpose: Computes profit margin
// Parameters:
// - totalCost: decimal.Decimal
// - totalRevenue: decimal.Decimal
// Logic:
1. Handles zero cost case
2. Calculates (revenue - cost) / cost
// Returns: decimal.Decimal (margin)
```

6. **Price Service (service/price.go)**
```go
// Key Functions:

// CalculateCost
// Purpose: Computes cost based on price model
// Parameters:
// - ctx: context.Context
// - price: *price.Price - Price configuration
// - quantity: decimal.Decimal - Usage amount
// Logic:
1. Handles different billing models:
   - Flat fee
   - Package-based
   - Tiered pricing
2. Applies transformations
3. Rounds to currency precision
// Returns: decimal.Decimal (cost)

// CalculateCostSheetPrice
// Purpose: Specialized cost calculation for cost sheets
// Parameters: Same as CalculateCost
// Logic: Reuses CalculateCost logic
// Returns: decimal.Decimal (cost)
```


7. CostSheetItem (DTO)

<svg aria-roledescription="classDiagram" role="graphics-document document" viewBox="0 0 995.6328125 406" style="max-width: 995.6328125px;" xmlns="http://www.w3.org/2000/svg" width="100%" id="mermaid-svg-1749566089096-xpenjxmrb"><style>#mermaid-svg-1749566089096-xpenjxmrb{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749566089096-xpenjxmrb .error-icon{fill:#bf616a;}#mermaid-svg-1749566089096-xpenjxmrb .error-text{fill:#bf616a;stroke:#bf616a;}#mermaid-svg-1749566089096-xpenjxmrb .edge-thickness-normal{stroke-width:2px;}#mermaid-svg-1749566089096-xpenjxmrb .edge-thickness-thick{stroke-width:3.5px;}#mermaid-svg-1749566089096-xpenjxmrb .edge-pattern-solid{stroke-dasharray:0;}#mermaid-svg-1749566089096-xpenjxmrb .edge-pattern-dashed{stroke-dasharray:3;}#mermaid-svg-1749566089096-xpenjxmrb .edge-pattern-dotted{stroke-dasharray:2;}#mermaid-svg-1749566089096-xpenjxmrb .marker{fill:rgba(204, 204, 204, 0.87);stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749566089096-xpenjxmrb .marker.cross{stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749566089096-xpenjxmrb svg{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;}#mermaid-svg-1749566089096-xpenjxmrb g.classGroup text{fill:#2a2a2a;stroke:none;font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:10px;}#mermaid-svg-1749566089096-xpenjxmrb g.classGroup text .title{font-weight:bolder;}#mermaid-svg-1749566089096-xpenjxmrb .nodeLabel,#mermaid-svg-1749566089096-xpenjxmrb .edgeLabel{color:#d8dee9;}#mermaid-svg-1749566089096-xpenjxmrb .edgeLabel .label rect{fill:#1a1a1a;}#mermaid-svg-1749566089096-xpenjxmrb .label text{fill:#d8dee9;}#mermaid-svg-1749566089096-xpenjxmrb .edgeLabel .label span{background:#1a1a1a;}#mermaid-svg-1749566089096-xpenjxmrb .classTitle{font-weight:bolder;}#mermaid-svg-1749566089096-xpenjxmrb .node rect,#mermaid-svg-1749566089096-xpenjxmrb .node circle,#mermaid-svg-1749566089096-xpenjxmrb .node ellipse,#mermaid-svg-1749566089096-xpenjxmrb .node polygon,#mermaid-svg-1749566089096-xpenjxmrb .node path{fill:#1a1a1a;stroke:#2a2a2a;stroke-width:1px;}#mermaid-svg-1749566089096-xpenjxmrb .divider{stroke:#2a2a2a;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb g.clickable{cursor:pointer;}#mermaid-svg-1749566089096-xpenjxmrb g.classGroup rect{fill:#1a1a1a;stroke:#2a2a2a;}#mermaid-svg-1749566089096-xpenjxmrb g.classGroup line{stroke:#2a2a2a;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb .classLabel .box{stroke:none;stroke-width:0;fill:#1a1a1a;opacity:0.5;}#mermaid-svg-1749566089096-xpenjxmrb .classLabel .label{fill:#2a2a2a;font-size:10px;}#mermaid-svg-1749566089096-xpenjxmrb .relation{stroke:rgba(204, 204, 204, 0.87);stroke-width:1;fill:none;}#mermaid-svg-1749566089096-xpenjxmrb .dashed-line{stroke-dasharray:3;}#mermaid-svg-1749566089096-xpenjxmrb .dotted-line{stroke-dasharray:1 2;}#mermaid-svg-1749566089096-xpenjxmrb #compositionStart,#mermaid-svg-1749566089096-xpenjxmrb .composition{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #compositionEnd,#mermaid-svg-1749566089096-xpenjxmrb .composition{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #dependencyStart,#mermaid-svg-1749566089096-xpenjxmrb .dependency{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #dependencyStart,#mermaid-svg-1749566089096-xpenjxmrb .dependency{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #extensionStart,#mermaid-svg-1749566089096-xpenjxmrb .extension{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #extensionEnd,#mermaid-svg-1749566089096-xpenjxmrb .extension{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #aggregationStart,#mermaid-svg-1749566089096-xpenjxmrb .aggregation{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #aggregationEnd,#mermaid-svg-1749566089096-xpenjxmrb .aggregation{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #lollipopStart,#mermaid-svg-1749566089096-xpenjxmrb .lollipop{fill:#1a1a1a!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb #lollipopEnd,#mermaid-svg-1749566089096-xpenjxmrb .lollipop{fill:#1a1a1a!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749566089096-xpenjxmrb .edgeTerminals{font-size:11px;}#mermaid-svg-1749566089096-xpenjxmrb .classTitleText{text-anchor:middle;font-size:18px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749566089096-xpenjxmrb :root{--mermaid-font-family:"trebuchet ms",verdana,arial,sans-serif;}</style><g><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker aggregation classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-aggregationStart"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker aggregation classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-aggregationEnd"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker extension classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-extensionStart"><path d="M 1,7 L18,13 V 1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker extension classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-extensionEnd"><path d="M 1,1 V 13 L18,7 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker composition classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-compositionStart"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker composition classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-compositionEnd"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="6" class="marker dependency classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-dependencyStart"><path d="M 5,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="13" class="marker dependency classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-dependencyEnd"><path d="M 18,7 L9,13 L14,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="13" class="marker lollipop classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-lollipopStart"><circle r="6" cy="7" cx="7" fill="transparent" stroke="black"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="1" class="marker lollipop classDiagram" id="mermaid-svg-1749566089096-xpenjxmrb_classDiagram-lollipopEnd"><circle r="6" cy="7" cx="7" fill="transparent" stroke="black"/></marker></defs><g class="root"><g class="clusters"/><g class="edgePaths"><path marker-end="url(#mermaid-svg-1749566089096-xpenjxmrb_classDiagram-dependencyEnd)" style="fill:none" class="edge-pattern-solid relation" id="id1" d="M561.594,166.75L561.594,180.292C561.594,193.833,561.594,220.917,561.594,237.625C561.594,254.333,561.594,260.667,561.594,263.833L561.594,267"/></g><g class="edgeLabels"><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g></g><g class="nodes"><g transform="translate(94.6015625, 115.5)" id="classId-CostSheetItem-22" class="node default"><rect height="192.5" width="173.203125" y="-96.25" x="-86.6015625" class="outer title-state"/><line y2="-65.75" y1="-65.75" x2="86.6015625" x1="-86.6015625" class="divider"/><line y2="85.25" y1="85.25" x2="86.6015625" x1="-86.6015625" class="divider"/><g class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel"></span></div></foreignObject><foreignObject transform="translate( -54.109375, -88.75)" height="18.5" width="108.21875" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CostSheetItem</span></div></foreignObject><foreignObject transform="translate( -79.1015625, -54.25)" height="18.5" width="67.8515625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+ID string</span></div></foreignObject><foreignObject transform="translate( -79.1015625, -31.75)" height="18.5" width="109.21875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+MeterID string</span></div></foreignObject><foreignObject transform="translate( -79.1015625, -9.25)" height="18.5" width="103.453125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+PriceID string</span></div></foreignObject><foreignObject transform="translate( -79.1015625, 13.25)" height="18.5" width="97.59375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Status string</span></div></foreignObject><foreignObject transform="translate( -79.1015625, 35.75)" height="18.5" width="116.109375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+TenantID string</span></div></foreignObject><foreignObject transform="translate( -79.1015625, 58.25)" height="18.5" width="158.203125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+EnvironmentID string</span></div></foreignObject></g></g><g transform="translate(317.8046875, 115.5)" id="classId-GetInputCostRequest-23" class="node default"><rect height="147.5" width="173.203125" y="-73.75" x="-86.6015625" class="outer title-state"/><line y2="-43.25" y1="-43.25" x2="86.6015625" x1="-86.6015625" class="divider"/><line y2="62.75" y1="62.75" x2="86.6015625" x1="-86.6015625" class="divider"/><g class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel"></span></div></foreignObject><foreignObject transform="translate( -78.78515625, -66.25)" height="18.5" width="157.5703125" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">GetInputCostRequest</span></div></foreignObject><foreignObject transform="translate( -79.1015625, -31.75)" height="18.5" width="157.3984375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+StartTime time.Time</span></div></foreignObject><foreignObject transform="translate( -79.1015625, -9.25)" height="18.5" width="148.6171875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+EndTime time.Time</span></div></foreignObject><foreignObject transform="translate( -79.1015625, 13.25)" height="18.5" width="116.109375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+TenantID string</span></div></foreignObject><foreignObject transform="translate( -79.1015625, 35.75)" height="18.5" width="158.203125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+EnvironmentID string</span></div></foreignObject></g></g><g transform="translate(561.59375, 115.5)" id="classId-GetInputCostResponse-24" class="node default"><rect height="102.5" width="214.375" y="-51.25" x="-107.1875" class="outer title-state"/><line y2="-20.75" y1="-20.75" x2="107.1875" x1="-107.1875" class="divider"/><line y2="40.25" y1="40.25" x2="107.1875" x1="-107.1875" class="divider"/><g class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel"></span></div></foreignObject><foreignObject transform="translate( -83.5703125, -43.75)" height="18.5" width="167.140625" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">GetInputCostResponse</span></div></foreignObject><foreignObject transform="translate( -99.6875, -9.25)" height="18.5" width="199.375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+TotalCost decimal.Decimal</span></div></foreignObject><foreignObject transform="translate( -99.6875, 13.25)" height="18.5" width="199.234375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Items[] CostSheetItemCost</span></div></foreignObject></g></g><g transform="translate(561.59375, 335.5)" id="classId-CostSheetItemCost-25" class="node default"><rect height="125" width="190.0625" y="-62.5" x="-95.03125" class="outer title-state"/><line y2="-32" y1="-32" x2="95.03125" x1="-95.03125" class="divider"/><line y2="51.5" y1="51.5" x2="95.03125" x1="-95.03125" class="divider"/><g class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel"></span></div></foreignObject><foreignObject transform="translate( -70.1484375, -55)" height="18.5" width="140.296875" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CostSheetItemCost</span></div></foreignObject><foreignObject transform="translate( -87.53125, -20.5)" height="18.5" width="109.21875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+MeterID string</span></div></foreignObject><foreignObject transform="translate( -87.53125, 2)" height="18.5" width="175.0625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Usage decimal.Decimal</span></div></foreignObject><foreignObject transform="translate( -87.53125, 24.5)" height="18.5" width="164.0234375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Cost decimal.Decimal</span></div></foreignObject></g></g><g transform="translate(853.20703125, 115.5)" id="classId-CostSheetFilter-26" class="node default"><rect height="215" width="268.8515625" y="-107.5" x="-134.42578125" class="outer title-state"/><line y2="-77" y1="-77" x2="134.42578125" x1="-134.42578125" class="divider"/><line y2="96.5" y1="96.5" x2="134.42578125" x1="-134.42578125" class="divider"/><g class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel"></span></div></foreignObject><foreignObject transform="translate( -57.83984375, -100)" height="18.5" width="115.6796875" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CostSheetFilter</span></div></foreignObject><foreignObject transform="translate( -126.92578125, -65.5)" height="18.5" width="181.8046875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+QueryFilter *QueryFilter</span></div></foreignObject><foreignObject transform="translate( -126.92578125, -43)" height="18.5" width="253.8515625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+TimeRangeFilter *TimeRangeFilter</span></div></foreignObject><foreignObject transform="translate( -126.92578125, -20.5)" height="18.5" width="183.859375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Filters[] *FilterCondition</span></div></foreignObject><foreignObject transform="translate( -126.92578125, 2)" height="18.5" width="157.1328125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Sort[] *SortCondition</span></div></foreignObject><foreignObject transform="translate( -126.92578125, 24.5)" height="18.5" width="157.2890625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CostSheetIDs[] string</span></div></foreignObject><foreignObject transform="translate( -126.92578125, 47)" height="18.5" width="116.109375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+TenantID string</span></div></foreignObject><foreignObject transform="translate( -126.92578125, 69.5)" height="18.5" width="158.203125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+EnvironmentID string</span></div></foreignObject></g></g></g></g></g></svg>


1. **CostSheetItem (DTO)**
```go
type CostSheetItem struct {
    // ID uniquely identifies the cost sheet item
    ID string `json:"id"`
    
    // MeterID references the usage meter associated with this cost item
    // Used to track what resource/service is being measured
    MeterID string `json:"meter_id"`
    
    // PriceID references the pricing configuration used for cost calculation
    // Links to the price model that determines how usage is converted to cost
    PriceID string `json:"price_id"`
    
    // Status indicates the current state of the cost sheet item
    // Typically "published" for active items
    Status string `json:"status"`
    
    // TenantID identifies the tenant this cost item belongs to
    // Used for multi-tenancy support
    TenantID string `json:"tenant_id"`
    
    // EnvironmentID specifies the environment context
    // Allows different pricing for different environments (e.g., prod vs staging)
    EnvironmentID string `json:"environment_id"`
}
```

2. **GetInputCostRequest (Request DTO)**
```go
type GetInputCostRequest struct {
    // StartTime defines the beginning of the time range for cost calculation
    // Used to determine the period for usage aggregation
    StartTime time.Time `json:"start_time"`
    
    // EndTime defines the end of the time range for cost calculation
    // Completes the time window for usage calculation
    EndTime time.Time `json:"end_time"`
    
    // TenantID identifies the tenant for which costs are being queried
    // Ensures cost calculations are tenant-specific
    TenantID string `json:"tenant_id"`
    
    // EnvironmentID specifies the environment context for the cost query
    // Allows environment-specific cost calculations
    EnvironmentID string `json:"environment_id"`
}
```

3. **GetInputCostResponse (Response DTO)**
```go
type GetInputCostResponse struct {
    // TotalCost represents the sum of all costs for the queried period
    // Aggregated cost across all items
    TotalCost decimal.Decimal `json:"total_cost"`
    
    // Items contains the detailed cost breakdown per meter
    // Provides itemized cost information
    Items []CostSheetItemCost `json:"items"`
}
```

4. **CostSheetItemCost (Cost Breakdown DTO)**
```go
type CostSheetItemCost struct {
    // MeterID identifies the specific usage meter
    // Links the cost to specific resource/service
    MeterID string `json:"meter_id"`
    
    // Usage represents the quantity or amount of resource consumed
    // Raw usage value before price calculation
    Usage decimal.Decimal `json:"usage"`
    
    // Cost represents the calculated cost based on usage and pricing
    // Final cost after applying pricing rules
    Cost decimal.Decimal `json:"cost"`
}
```

5. **CostSheetFilter (Query Filter DTO)**
```go
type CostSheetFilter struct {
    // QueryFilter contains general query parameters and pagination settings
    // Handles basic query configuration
    QueryFilter *QueryFilter `json:"query_filter"`
    
    // TimeRangeFilter specifies the time period for which to retrieve cost data
    // Allows time-based filtering
    TimeRangeFilter *TimeRangeFilter `json:"time_range_filter"`
    
    // Filters contains an array of specific filtering conditions
    // Enables complex query criteria
    Filters []*FilterCondition `json:"filters"`
    
    // Sort specifies the ordering preferences for the results
    // Controls result ordering
    Sort []*SortCondition `json:"sort"`
    
    // CostSheetIDs allows filtering by specific cost sheet identifiers
    // Direct ID-based filtering
    CostSheetIDs []string `json:"cost_sheet_ids"`
    
    // TenantID filters results for a specific tenant
    // Tenant-specific filtering
    TenantID string `json:"tenant_id"`
    
    // EnvironmentID filters results for a specific environment
    // Environment-specific filtering
    EnvironmentID string `json:"environment_id"`
}
```

**Integration with Other Components:**

1. **With Domain Model:**
   ```go
   // Domain model uses these DTOs for external communication
   func (c *CostSheetItem) ToDTO() *types.CostSheetItem {
       // Converts domain model to DTO for API responses
   }
   
   func FromDTO(dto *types.CostSheetItem) *CostSheetItem {
       // Creates domain model from DTO for internal processing
   }
   ```

2. **With Service Layer:**
   ```go
   // CostSheetService uses these types for request/response handling
   func (s *costSheetService) GetInputCostForMargin(
       ctx context.Context, 
       req *types.GetInputCostRequest,
   ) (*types.GetInputCostResponse, error) {
       // Processes request DTO and returns response DTO
   }
   ```

3. **With Repository Layer:**
   ```go
   // Repository uses CostSheetFilter for querying
   func (r *repository) List(
       ctx context.Context, 
       filter *types.CostSheetFilter,
   ) ([]*CostSheetItem, error) {
       // Uses filter DTO for database queries
   }
   ```

These types serve as the contract between different layers of the application:
- They define the structure of data moving between layers
- Provide validation through struct tags
- Enable clean serialization/deserialization for API communication
- Support filtering and pagination for data queries

The DTOs in `cost_sheet.go` are crucial for:
1. API Request/Response handling
2. Data filtering and querying
3. Cost calculation results
4. Cross-layer communication

**Complete Workflow Example:**
When calculating input cost for margin:

1. **Request Initiation**
```go
req := &types.GetInputCostRequest{
    StartTime: startTime,
    EndTime: endTime,
    TenantID: "tenant123",
    EnvironmentID: "env456"
}
```

2. **Repository Access**
```go
filter := &types.CostSheetFilter{
    TenantID: req.TenantID,
    EnvironmentID: req.EnvironmentID,
    Filters: []*types.FilterCondition{
        {Field: "status", Operator: "=", Value: "published"}
    }
}
items, err := costSheetRepo.List(ctx, filter)
```

3. **Usage Data Collection**
```go
usageRequests := make([]*dto.GetUsageByMeterRequest, len(items))
for i, item := range items {
    usageRequests[i] = &dto.GetUsageByMeterRequest{
        MeterID: item.MeterID,
        StartTime: req.StartTime,
        EndTime: req.EndTime
    }
}
usageData := eventService.BulkGetUsageByMeter(ctx, usageRequests)
```

4. **Cost Calculation**
```go
for _, item := range items {
    price := priceService.GetPrice(ctx, item.PriceID)
    usage := usageData[item.MeterID]
    cost := priceService.CalculateCostSheetPrice(ctx, price, usage.Value)
    totalCost = totalCost.Add(cost)
}
```

5. **Response Generation**
```go
response := &types.GetInputCostResponse{
    TotalCost: totalCost,
    Items: itemCosts
}
```

This workflow demonstrates how all components work together to:
1. Access data through the repository
2. Process business logic in services
3. Calculate costs using specialized pricing logic
4. Aggregate results for the response



### _Visuals_

<svg aria-roledescription="flowchart-v2" role="graphics-document document" viewBox="-8 -8 1510.34375 226.5" style="max-width: 1510.34375px;" xmlns="http://www.w3.org/2000/svg" width="100%" id="mermaid-svg-1749560002583-e1yhvawqw"><style>#mermaid-svg-1749560002583-e1yhvawqw{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749560002583-e1yhvawqw .error-icon{fill:#bf616a;}#mermaid-svg-1749560002583-e1yhvawqw .error-text{fill:#bf616a;stroke:#bf616a;}#mermaid-svg-1749560002583-e1yhvawqw .edge-thickness-normal{stroke-width:2px;}#mermaid-svg-1749560002583-e1yhvawqw .edge-thickness-thick{stroke-width:3.5px;}#mermaid-svg-1749560002583-e1yhvawqw .edge-pattern-solid{stroke-dasharray:0;}#mermaid-svg-1749560002583-e1yhvawqw .edge-pattern-dashed{stroke-dasharray:3;}#mermaid-svg-1749560002583-e1yhvawqw .edge-pattern-dotted{stroke-dasharray:2;}#mermaid-svg-1749560002583-e1yhvawqw .marker{fill:rgba(204, 204, 204, 0.87);stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749560002583-e1yhvawqw .marker.cross{stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749560002583-e1yhvawqw svg{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;}#mermaid-svg-1749560002583-e1yhvawqw .label{font-family:"trebuchet ms",verdana,arial,sans-serif;color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749560002583-e1yhvawqw .cluster-label text{fill:#ffffff;}#mermaid-svg-1749560002583-e1yhvawqw .cluster-label span,#mermaid-svg-1749560002583-e1yhvawqw p{color:#ffffff;}#mermaid-svg-1749560002583-e1yhvawqw .label text,#mermaid-svg-1749560002583-e1yhvawqw span,#mermaid-svg-1749560002583-e1yhvawqw p{fill:rgba(204, 204, 204, 0.87);color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749560002583-e1yhvawqw .node rect,#mermaid-svg-1749560002583-e1yhvawqw .node circle,#mermaid-svg-1749560002583-e1yhvawqw .node ellipse,#mermaid-svg-1749560002583-e1yhvawqw .node polygon,#mermaid-svg-1749560002583-e1yhvawqw .node path{fill:#1a1a1a;stroke:#2a2a2a;stroke-width:1px;}#mermaid-svg-1749560002583-e1yhvawqw .flowchart-label text{text-anchor:middle;}#mermaid-svg-1749560002583-e1yhvawqw .node .label{text-align:center;}#mermaid-svg-1749560002583-e1yhvawqw .node.clickable{cursor:pointer;}#mermaid-svg-1749560002583-e1yhvawqw .arrowheadPath{fill:#e5e5e5;}#mermaid-svg-1749560002583-e1yhvawqw .edgePath .path{stroke:rgba(204, 204, 204, 0.87);stroke-width:2.0px;}#mermaid-svg-1749560002583-e1yhvawqw .flowchart-link{stroke:rgba(204, 204, 204, 0.87);fill:none;}#mermaid-svg-1749560002583-e1yhvawqw .edgeLabel{background-color:#1a1a1a99;text-align:center;}#mermaid-svg-1749560002583-e1yhvawqw .edgeLabel rect{opacity:0.5;background-color:#1a1a1a99;fill:#1a1a1a99;}#mermaid-svg-1749560002583-e1yhvawqw .labelBkg{background-color:rgba(26, 26, 26, 0.5);}#mermaid-svg-1749560002583-e1yhvawqw .cluster rect{fill:rgba(64, 64, 64, 0.47);stroke:#30373a;stroke-width:1px;}#mermaid-svg-1749560002583-e1yhvawqw .cluster text{fill:#ffffff;}#mermaid-svg-1749560002583-e1yhvawqw .cluster span,#mermaid-svg-1749560002583-e1yhvawqw p{color:#ffffff;}#mermaid-svg-1749560002583-e1yhvawqw div.mermaidTooltip{position:absolute;text-align:center;max-width:200px;padding:2px;font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:12px;background:#88c0d0;border:1px solid #30373a;border-radius:2px;pointer-events:none;z-index:100;}#mermaid-svg-1749560002583-e1yhvawqw .flowchartTitleText{text-anchor:middle;font-size:18px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749560002583-e1yhvawqw :root{--mermaid-font-family:"trebuchet ms",verdana,arial,sans-serif;}</style><g><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="6" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 0 L 10 5 L 0 10 z"/></marker><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="4.5" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointStart"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 5 L 10 10 L 10 0 z"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="11" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749560002583-e1yhvawqw_flowchart-circleEnd"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="-1" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749560002583-e1yhvawqw_flowchart-circleStart"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="12" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749560002583-e1yhvawqw_flowchart-crossEnd"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="-1" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749560002583-e1yhvawqw_flowchart-crossStart"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><g class="root"><g class="clusters"><g id="subGraph3" class="cluster default flowchart-label"><rect height="210.5" width="327.69921875" y="0" x="0" ry="0" rx="0" style=""/><g transform="translate(115.923828125, 0)" class="cluster-label"><foreignObject height="18.5" width="95.8515625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Service Layer</span></div></foreignObject></g></g><g id="subGraph2" class="cluster default flowchart-label"><rect height="83.5" width="216.4296875" y="0" x="919.9453125" ry="0" rx="0" style=""/><g transform="translate(956.34375, 0)" class="cluster-label"><foreignObject height="18.5" width="143.6328125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Infrastructure Layer</span></div></foreignObject></g></g><g id="subGraph1" class="cluster default flowchart-label"><rect height="210.5" width="552.24609375" y="0" x="347.69921875" ry="0" rx="0" style=""/><g transform="translate(575.044921875, 0)" class="cluster-label"><foreignObject height="18.5" width="97.5546875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Domain Layer</span></div></foreignObject></g></g><g id="Database" class="cluster default flowchart-label"><rect height="210.5" width="337.96875" y="0" x="1156.375" ry="0" rx="0" style=""/><g transform="translate(1292.61328125, 0)" class="cluster-label"><foreignObject height="18.5" width="65.4921875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Database</span></div></foreignObject></g></g></g><g class="edgePaths"><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-A LE-B" id="L-A-B-0" d="M1400.484,58.5L1400.484,62.667C1400.484,66.833,1400.484,75.167,1400.484,85.042C1400.484,94.917,1400.484,106.333,1390.926,117.323C1381.368,128.312,1362.251,138.875,1352.693,144.156L1343.134,149.437"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C LE-D" id="L-C-D-0" d="M494.871,58.5L494.871,62.667C494.871,66.833,494.871,75.167,494.871,85.042C494.871,94.917,494.871,106.333,494.871,116.867C494.871,127.4,494.871,137.05,494.871,141.875L494.871,146.7"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-E LE-F" id="L-E-F-0" d="M810.914,58.5L810.914,62.667C810.914,66.833,810.914,75.167,810.914,85.042C810.914,94.917,810.914,106.333,803.386,117.248C795.858,128.162,780.801,138.574,773.273,143.78L765.745,148.985"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-G LE-F" id="L-G-F-0" d="M1010.464,58.5L1006.062,62.667C1001.66,66.833,992.857,75.167,944.879,85.042C896.902,94.917,809.752,106.333,767.564,116.901C725.376,127.468,728.151,137.186,729.539,142.045L730.926,146.904"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-G LE-B" id="L-G-B-0" d="M1045.856,58.5L1050.258,62.667C1054.66,66.833,1063.464,75.167,1107.184,85.042C1150.905,94.917,1229.542,106.333,1268.861,116.867C1308.18,127.4,1308.18,137.05,1308.18,141.875L1308.18,146.7"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-H LE-F" id="L-H-F-0" d="M214.922,58.5L226.031,62.667C237.14,66.833,259.357,75.167,270.466,85.042C281.574,94.917,281.574,106.333,343.274,118.949C404.975,131.564,528.375,145.378,590.076,152.285L651.776,159.191"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-H LE-I" id="L-H-I-0" d="M142.616,58.5L135.738,62.667C128.86,66.833,115.104,75.167,108.226,85.042C101.348,94.917,101.348,106.333,101.348,116.867C101.348,127.4,101.348,137.05,101.348,141.875L101.348,146.7"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-H LE-D" id="L-H-D-0" d="M163.5,58.5L161.817,62.667C160.134,66.833,156.768,75.167,155.085,85.042C153.402,94.917,153.402,106.333,190.749,117.62C228.095,128.906,302.788,140.061,340.134,145.639L377.48,151.217"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-B LE-G" id="L-B-G-0" d="M1282.277,152L1273.45,146.292C1264.622,140.583,1246.967,129.167,1201.959,117.75C1156.951,106.333,1084.59,94.917,1049.684,85.867C1014.779,76.817,1017.329,70.134,1018.604,66.793L1019.879,63.452"/><path marker-end="url(#mermaid-svg-1749560002583-e1yhvawqw_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-H" id="L-F-H-0" d="M657.043,160.887L583.784,153.698C510.525,146.508,364.007,132.129,290.747,119.231C217.488,106.333,217.488,94.917,213.437,85.627C209.386,76.337,201.284,69.174,197.233,65.592L193.182,62.011"/></g><g class="edgeLabels"><g transform="translate(1400.484375, 117.75)" class="edgeLabel"><g transform="translate(-56.27734375, -9.25)" class="label"><foreignObject height="18.5" width="112.5546875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Defines Schema</span></div></foreignObject></g></g><g transform="translate(494.87109375, 117.75)" class="edgeLabel"><g transform="translate(-26.48046875, -9.25)" class="label"><foreignObject height="18.5" width="52.9609375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Defines</span></div></foreignObject></g></g><g transform="translate(810.9140625, 117.75)" class="edgeLabel"><g transform="translate(-26.48046875, -9.25)" class="label"><foreignObject height="18.5" width="52.9609375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Defines</span></div></foreignObject></g></g><g transform="translate(722.6015625, 117.75)" class="edgeLabel"><g transform="translate(-41.83203125, -9.25)" class="label"><foreignObject height="18.5" width="83.6640625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Implements</span></div></foreignObject></g></g><g transform="translate(1308.1796875, 117.75)" class="edgeLabel"><g transform="translate(-16.02734375, -9.25)" class="label"><foreignObject height="18.5" width="32.0546875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Uses</span></div></foreignObject></g></g><g transform="translate(281.57421875, 117.75)" class="edgeLabel"><g transform="translate(-16.02734375, -9.25)" class="label"><foreignObject height="18.5" width="32.0546875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Uses</span></div></foreignObject></g></g><g transform="translate(101.34765625, 117.75)" class="edgeLabel"><g transform="translate(-16.02734375, -9.25)" class="label"><foreignObject height="18.5" width="32.0546875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Uses</span></div></foreignObject></g></g><g transform="translate(153.40234375, 117.75)" class="edgeLabel"><g transform="translate(-16.02734375, -9.25)" class="label"><foreignObject height="18.5" width="32.0546875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Uses</span></div></foreignObject></g></g><g transform="translate(1229.3125, 117.75)" class="edgeLabel"><g transform="translate(-42.83984375, -9.25)" class="label"><foreignObject height="18.5" width="85.6796875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Data Source</span></div></foreignObject></g></g><g transform="translate(217.48828125, 117.75)" class="edgeLabel"><g transform="translate(-28.05859375, -9.25)" class="label"><foreignObject height="18.5" width="56.1171875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel">Used by</span></div></foreignObject></g></g></g><g class="nodes"><g transform="translate(170.265625, 41.75)" id="flowchart-H-200" class="node default default flowchart-label"><rect height="33.5" width="173.0859375" y="-16.75" x="-86.54296875" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-79.04296875, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="158.0859375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">service/cost_sheet.go</span></div></foreignObject></g></g><g transform="translate(101.34765625, 168.75)" id="flowchart-I-203" class="node default default flowchart-label"><rect height="33.5" width="132.6953125" y="-16.75" x="-66.34765625" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-58.84765625, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="117.6953125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">service/price.go</span></div></foreignObject></g></g><g transform="translate(1028.16015625, 41.75)" id="flowchart-G-196" class="node default default flowchart-label"><rect height="33.5" width="146.4296875" y="-16.75" x="-73.21484375" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-65.71484375, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="131.4296875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">ent/cost_sheet.go</span></div></foreignObject></g></g><g transform="translate(494.87109375, 168.75)" id="flowchart-D-193" class="node default default flowchart-label"><rect height="33.5" width="224.34375" y="-16.75" x="-112.171875" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-104.671875, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="209.34375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CostSheetItem Domain Model</span></div></foreignObject></g></g><g transform="translate(494.87109375, 41.75)" id="flowchart-C-192" class="node default default flowchart-label"><rect height="33.5" width="81.71875" y="-16.75" x="-40.859375" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-33.359375, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="66.71875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">model.go</span></div></foreignObject></g></g><g transform="translate(737.1640625, 168.75)" id="flowchart-F-195" class="node default default flowchart-label"><rect height="33.5" width="160.2421875" y="-16.75" x="-80.12109375" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-72.62109375, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="145.2421875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Repository Interface</span></div></foreignObject></g></g><g transform="translate(810.9140625, 41.75)" id="flowchart-E-194" class="node default default flowchart-label"><rect height="33.5" width="108.0625" y="-16.75" x="-54.03125" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-46.53125, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="93.0625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">repository.go</span></div></foreignObject></g></g><g transform="translate(1308.1796875, 168.75)" id="flowchart-B-191" class="node default default flowchart-label"><rect height="33.5" width="181.4453125" y="-16.75" x="-90.72265625" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-83.22265625, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="166.4453125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">cost_sheet_items table</span></div></foreignObject></g></g><g transform="translate(1400.484375, 41.75)" id="flowchart-A-190" class="node default default flowchart-label"><rect height="33.5" width="117.71875" y="-16.75" x="-58.859375" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-51.359375, -9.25)" style="" class="label"><rect/><foreignObject height="18.5" width="102.71875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">cost_sheet.sql</span></div></foreignObject></g></g></g></g></g></svg>





### _Detailed function map_


<svg aria-roledescription="classDiagram" role="graphics-document document" viewBox="0 0 860.9609375 716" style="max-width: 860.9609375px;" xmlns="http://www.w3.org/2000/svg" width="100%" id="mermaid-svg-1749564052977-m773awpm2"><style>#mermaid-svg-1749564052977-m773awpm2{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749564052977-m773awpm2 .error-icon{fill:#bf616a;}#mermaid-svg-1749564052977-m773awpm2 .error-text{fill:#bf616a;stroke:#bf616a;}#mermaid-svg-1749564052977-m773awpm2 .edge-thickness-normal{stroke-width:2px;}#mermaid-svg-1749564052977-m773awpm2 .edge-thickness-thick{stroke-width:3.5px;}#mermaid-svg-1749564052977-m773awpm2 .edge-pattern-solid{stroke-dasharray:0;}#mermaid-svg-1749564052977-m773awpm2 .edge-pattern-dashed{stroke-dasharray:3;}#mermaid-svg-1749564052977-m773awpm2 .edge-pattern-dotted{stroke-dasharray:2;}#mermaid-svg-1749564052977-m773awpm2 .marker{fill:rgba(204, 204, 204, 0.87);stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749564052977-m773awpm2 .marker.cross{stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749564052977-m773awpm2 svg{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;}#mermaid-svg-1749564052977-m773awpm2 g.classGroup text{fill:#2a2a2a;stroke:none;font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:10px;}#mermaid-svg-1749564052977-m773awpm2 g.classGroup text .title{font-weight:bolder;}#mermaid-svg-1749564052977-m773awpm2 .nodeLabel,#mermaid-svg-1749564052977-m773awpm2 .edgeLabel{color:#d8dee9;}#mermaid-svg-1749564052977-m773awpm2 .edgeLabel .label rect{fill:#1a1a1a;}#mermaid-svg-1749564052977-m773awpm2 .label text{fill:#d8dee9;}#mermaid-svg-1749564052977-m773awpm2 .edgeLabel .label span{background:#1a1a1a;}#mermaid-svg-1749564052977-m773awpm2 .classTitle{font-weight:bolder;}#mermaid-svg-1749564052977-m773awpm2 .node rect,#mermaid-svg-1749564052977-m773awpm2 .node circle,#mermaid-svg-1749564052977-m773awpm2 .node ellipse,#mermaid-svg-1749564052977-m773awpm2 .node polygon,#mermaid-svg-1749564052977-m773awpm2 .node path{fill:#1a1a1a;stroke:#2a2a2a;stroke-width:1px;}#mermaid-svg-1749564052977-m773awpm2 .divider{stroke:#2a2a2a;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 g.clickable{cursor:pointer;}#mermaid-svg-1749564052977-m773awpm2 g.classGroup rect{fill:#1a1a1a;stroke:#2a2a2a;}#mermaid-svg-1749564052977-m773awpm2 g.classGroup line{stroke:#2a2a2a;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 .classLabel .box{stroke:none;stroke-width:0;fill:#1a1a1a;opacity:0.5;}#mermaid-svg-1749564052977-m773awpm2 .classLabel .label{fill:#2a2a2a;font-size:10px;}#mermaid-svg-1749564052977-m773awpm2 .relation{stroke:rgba(204, 204, 204, 0.87);stroke-width:1;fill:none;}#mermaid-svg-1749564052977-m773awpm2 .dashed-line{stroke-dasharray:3;}#mermaid-svg-1749564052977-m773awpm2 .dotted-line{stroke-dasharray:1 2;}#mermaid-svg-1749564052977-m773awpm2 #compositionStart,#mermaid-svg-1749564052977-m773awpm2 .composition{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #compositionEnd,#mermaid-svg-1749564052977-m773awpm2 .composition{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #dependencyStart,#mermaid-svg-1749564052977-m773awpm2 .dependency{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #dependencyStart,#mermaid-svg-1749564052977-m773awpm2 .dependency{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #extensionStart,#mermaid-svg-1749564052977-m773awpm2 .extension{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #extensionEnd,#mermaid-svg-1749564052977-m773awpm2 .extension{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #aggregationStart,#mermaid-svg-1749564052977-m773awpm2 .aggregation{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #aggregationEnd,#mermaid-svg-1749564052977-m773awpm2 .aggregation{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #lollipopStart,#mermaid-svg-1749564052977-m773awpm2 .lollipop{fill:#1a1a1a!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #lollipopEnd,#mermaid-svg-1749564052977-m773awpm2 .lollipop{fill:#1a1a1a!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 .edgeTerminals{font-size:11px;}#mermaid-svg-1749564052977-m773awpm2 .classTitleText{text-anchor:middle;font-size:18px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749564052977-m773awpm2 :root{--mermaid-font-family:"trebuchet ms",verdana,arial,sans-serif;}</style><g><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker aggregation classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-aggregationStart"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker aggregation classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-aggregationEnd"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker extension classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-extensionStart"><path d="M 1,7 L18,13 V 1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker extension classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-extensionEnd"><path d="M 1,1 V 13 L18,7 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker composition classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-compositionStart"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker composition classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-compositionEnd"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="6" class="marker dependency classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyStart"><path d="M 5,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="13" class="marker dependency classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyEnd"><path d="M 18,7 L9,13 L14,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="13" class="marker lollipop classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-lollipopStart"><circle r="6" cy="7" cx="7" fill="transparent" stroke="black"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="1" class="marker lollipop classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-lollipopEnd"><circle r="6" cy="7" cx="7" fill="transparent" stroke="black"/></marker></defs><g class="root"><g class="clusters"/><g class="edgePaths"><path marker-end="url(#mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyEnd)" style="fill:none" class="edge-pattern-solid relation" id="id1" d="M226.996,133L216.342,137.167C205.688,141.333,184.379,149.667,173.725,157C163.07,164.333,163.07,170.667,163.07,173.833L163.07,177"/><path marker-end="url(#mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyEnd)" style="fill:none" class="edge-pattern-solid relation" id="id2" d="M546.625,133L557.279,137.167C567.934,141.333,589.242,149.667,599.896,164.5C610.551,179.333,610.551,200.667,610.551,211.333L610.551,222"/><path marker-end="url(#mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyEnd)" style="fill:none" class="edge-pattern-solid relation" id="id3" d="M163.07,398L163.07,402.167C163.07,406.333,163.07,414.667,163.07,422C163.07,429.333,163.07,435.667,163.07,438.833L163.07,442"/></g><g class="edgeLabels"><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g></g><g class="nodes"><g transform="translate(163.0703125, 578)" id="classId-CostSheetItem-8" class="node default"><rect height="260" width="249.65625" y="-130" x="-124.828125" class="outer title-state"/><line y2="-99.5" y1="-99.5" x2="124.828125" x1="-124.828125" class="divider"/><line y2="96.5" y1="96.5" x2="124.828125" x1="-124.828125" class="divider"/><g class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel"></span></div></foreignObject><foreignObject transform="translate( -54.109375, -122.5)" height="18.5" width="108.21875" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CostSheetItem</span></div></foreignObject><foreignObject transform="translate( -117.328125, -88)" height="18.5" width="67.8515625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+ID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, -65.5)" height="18.5" width="109.21875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+MeterID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, -43)" height="18.5" width="103.453125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+PriceID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, -20.5)" height="18.5" width="97.59375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Status string</span></div></foreignObject><foreignObject transform="translate( -117.328125, 2)" height="18.5" width="116.109375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+TenantID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, 24.5)" height="18.5" width="158.203125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+EnvironmentID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, 47)" height="18.5" width="159.8828125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CreatedAt time.Time</span></div></foreignObject><foreignObject transform="translate( -117.328125, 69.5)" height="18.5" width="163.5703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+UpdatedAt time.Time</span></div></foreignObject><foreignObject transform="translate( -117.328125, 104)" height="18.5" width="234.65625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+ToDTO() : *types.CostSheetItem</span></div></foreignObject></g></g><g transform="translate(163.0703125, 290.5)" id="classId-Repository-9" class="node default"><rect height="215" width="310.140625" y="-107.5" x="-155.0703125" class="outer title-state"/><line y2="-54.5" y1="-54.5" x2="155.0703125" x1="-155.0703125" class="divider"/><line y2="-38.5" y1="-38.5" x2="155.0703125" x1="-155.0703125" class="divider"/><g class="label"><foreignObject transform="translate( -41.171875, -100)" height="18.5" width="82.34375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">«interface»</span></div></foreignObject><foreignObject transform="translate( -39.890625, -77.5)" height="18.5" width="79.78125" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Repository</span></div></foreignObject><foreignObject transform="translate( -147.5703125, -31)" height="18.5" width="185.5078125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Create(ctx, item) : error</span></div></foreignObject><foreignObject transform="translate( -147.5703125, -8.5)" height="18.5" width="260.7890625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Get(ctx, id)(*CostSheetItem, error)</span></div></foreignObject><foreignObject transform="translate( -147.5703125, 14)" height="18.5" width="295.140625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+List(ctx, filter)([]*CostSheetItem, error)</span></div></foreignObject><foreignObject transform="translate( -147.5703125, 36.5)" height="18.5" width="209.6484375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Count(ctx, filter)(int, error)</span></div></foreignObject><foreignObject transform="translate( -147.5703125, 59)" height="18.5" width="189.1953125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Update(ctx, item) : error</span></div></foreignObject><foreignObject transform="translate( -147.5703125, 81.5)" height="18.5" width="165.1328125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Delete(ctx, id) : error</span></div></foreignObject></g></g><g transform="translate(386.810546875, 70.5)" id="classId-CostSheetService-10" class="node default"><rect height="125" width="480.21875" y="-62.5" x="-240.109375" class="outer title-state"/><line y2="-9.5" y1="-9.5" x2="240.109375" x1="-240.109375" class="divider"/><line y2="6.5" y1="6.5" x2="240.109375" x1="-240.109375" class="divider"/><g class="label"><foreignObject transform="translate( -41.171875, -55)" height="18.5" width="82.34375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">«interface»</span></div></foreignObject><foreignObject transform="translate( -64.890625, -32.5)" height="18.5" width="129.78125" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CostSheetService</span></div></foreignObject><foreignObject transform="translate( -232.609375, 14)" height="18.5" width="465.21875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+GetInputCostForMargin(ctx, req)(*GetInputCostResponse, error)</span></div></foreignObject><foreignObject transform="translate( -232.609375, 36.5)" height="18.5" width="440.9921875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CalculateMargin(totalCost, totalRevenue) : decimal.Decimal</span></div></foreignObject></g></g><g transform="translate(610.55078125, 290.5)" id="classId-PriceService-11" class="node default"><rect height="125" width="484.8203125" y="-62.5" x="-242.41015625" class="outer title-state"/><line y2="-9.5" y1="-9.5" x2="242.41015625" x1="-242.41015625" class="divider"/><line y2="6.5" y1="6.5" x2="242.41015625" x1="-242.41015625" class="divider"/><g class="label"><foreignObject transform="translate( -41.171875, -55)" height="18.5" width="82.34375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">«interface»</span></div></foreignObject><foreignObject transform="translate( -46.84375, -32.5)" height="18.5" width="93.6875" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">PriceService</span></div></foreignObject><foreignObject transform="translate( -234.91015625, 14)" height="18.5" width="393.984375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CalculateCost(ctx, price, quantity) : decimal.Decimal</span></div></foreignObject><foreignObject transform="translate( -234.91015625, 36.5)" height="18.5" width="469.8203125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CalculateCostSheetPrice(ctx, price, quantity) : decimal.Decimal</span></div></foreignObject></g></g></g></g></g></svg>