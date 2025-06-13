# Cost Sheet PRD

### _Cost Sheet Integration with Billing System_

## 1. Overview

The Cost Sheet feature enables tracking and calculating input costs within the billing system. It provides a foundation for margin analysis by integrating with existing usage tracking and price calculation infrastructure. The system maintains a clear separation between revenue calculations and cost tracking while leveraging shared components.

## 2. Core Architecture

### 2.1 Database Schema

```sql
CREATE TABLE public.costsheet (
    id              VARCHAR(50) DEFAULT extensions.uuid_generate_v4() PRIMARY KEY,
    meter_id        VARCHAR(50) NOT NULL,
    price_id        VARCHAR(50) NOT NULL,
    tenant_id       VARCHAR(50) NOT NULL,
    environment_id  VARCHAR(50) NOT NULL,
    status          VARCHAR(20) DEFAULT 'published' NOT NULL,
    created_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(50),
    updated_by      VARCHAR(50),
    FOREIGN KEY (meter_id)         REFERENCES meters(id),
    FOREIGN KEY (price_id)         REFERENCES prices(id),
    FOREIGN KEY (tenant_id)        REFERENCES tenants(id),
    FOREIGN KEY (environment_id)   REFERENCES environments(id)
);

-- Indexes for query optimization
CREATE INDEX idx_costsheet_tenant_env ON costsheet(tenant_id, environment_id);
CREATE UNIQUE INDEX idx_costsheet_meter_price ON costsheet(meter_id, price_id) WHERE status = 'published';
```

### 2.2 Domain Model

The cost sheet implementation follows a clean domain-driven design:

1. **Domain Model (`Costsheet`)**
   ```go
   type Costsheet struct {
       ID string
       MeterID string
       PriceID string
       types.BaseModel  // Embeds common fields
   }
   ```

2. **Repository Interface**
   ```go
   type Repository interface {
       Create(ctx context.Context, costsheet *Costsheet) error
       Get(ctx context.Context, id string) (*Costsheet, error)
       Update(ctx context.Context, costsheet *Costsheet) error
       Delete(ctx context.Context, id string) error
       List(ctx context.Context, filter *Filter) ([]*Costsheet, error)
       GetByMeterAndPrice(ctx context.Context, meterID, priceID string) (*Costsheet, error)
   }
   ```

3. **Filter Structure**
   ```go
   type Filter struct {
       QueryFilter *types.QueryFilter
       TimeRangeFilter *types.TimeRangeFilter
       Filters []*types.FilterCondition
       Sort []*types.SortCondition
       CostsheetIDs []string
       MeterIDs []string
       PriceIDs []string
       Status types.CostsheetStatus
       TenantID string
       EnvironmentID string
   }
   ```

### 2.3 Integration Points

1. **Meter Service**
   - Provides usage data via BulkGetUsageByMeter
   - Tracks consumption for cost calculation

2. **Price Service**
   - Handles cost calculations using existing price models
   - Supports flat rate, tiered, and package pricing

3. **Tenant & Environment Context**
   - Multi-tenancy support through tenant_id
   - Environment isolation (prod, staging, etc.)


### 2.4 Core Functions

1. **GetInputCostForMargin**
   - Fetches all published cost items
   - Gets usage data via BulkGetUsageByMeter
   - Calculates costs using price service
   - Returns aggregated input costs

2. **CalculateMargin**
   - Input: TotalInputCost, TotalRevenue
   - Calculates: (TotalRevenue - TotalCost) / TotalCost
   - Returns margin percentage

## 2.5. Service Interfaces

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

## 2.6 Cost Calculation Flow


<svg aria-roledescription="flowchart-v2" role="graphics-document document" viewBox="-8 -8 593.796875 467.60003662109375" style="max-width: 593.796875px;" xmlns="http://www.w3.org/2000/svg" width="100%" id="mermaid-svg-1749529562038-tp0jz51sh"><style>#mermaid-svg-1749529562038-tp0jz51sh{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh .error-icon{fill:#bf616a;}#mermaid-svg-1749529562038-tp0jz51sh .error-text{fill:#bf616a;stroke:#bf616a;}#mermaid-svg-1749529562038-tp0jz51sh .edge-thickness-normal{stroke-width:2px;}#mermaid-svg-1749529562038-tp0jz51sh .edge-thickness-thick{stroke-width:3.5px;}#mermaid-svg-1749529562038-tp0jz51sh .edge-pattern-solid{stroke-dasharray:0;}#mermaid-svg-1749529562038-tp0jz51sh .edge-pattern-dashed{stroke-dasharray:3;}#mermaid-svg-1749529562038-tp0jz51sh .edge-pattern-dotted{stroke-dasharray:2;}#mermaid-svg-1749529562038-tp0jz51sh .marker{fill:rgba(204, 204, 204, 0.87);stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh .marker.cross{stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh svg{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;}#mermaid-svg-1749529562038-tp0jz51sh .label{font-family:"trebuchet ms",verdana,arial,sans-serif;color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh .cluster-label text{fill:#ffffff;}#mermaid-svg-1749529562038-tp0jz51sh .cluster-label span,#mermaid-svg-1749529562038-tp0jz51sh p{color:#ffffff;}#mermaid-svg-1749529562038-tp0jz51sh .label text,#mermaid-svg-1749529562038-tp0jz51sh span,#mermaid-svg-1749529562038-tp0jz51sh p{fill:rgba(204, 204, 204, 0.87);color:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh .node rect,#mermaid-svg-1749529562038-tp0jz51sh .node circle,#mermaid-svg-1749529562038-tp0jz51sh .node ellipse,#mermaid-svg-1749529562038-tp0jz51sh .node polygon,#mermaid-svg-1749529562038-tp0jz51sh .node path{fill:#1a1a1a;stroke:#2a2a2a;stroke-width:1px;}#mermaid-svg-1749529562038-tp0jz51sh .flowchart-label text{text-anchor:middle;}#mermaid-svg-1749529562038-tp0jz51sh .node .label{text-align:center;}#mermaid-svg-1749529562038-tp0jz51sh .node.clickable{cursor:pointer;}#mermaid-svg-1749529562038-tp0jz51sh .arrowheadPath{fill:#e5e5e5;}#mermaid-svg-1749529562038-tp0jz51sh .edgePath .path{stroke:rgba(204, 204, 204, 0.87);stroke-width:2.0px;}#mermaid-svg-1749529562038-tp0jz51sh .flowchart-link{stroke:rgba(204, 204, 204, 0.87);fill:none;}#mermaid-svg-1749529562038-tp0jz51sh .edgeLabel{background-color:#1a1a1a99;text-align:center;}#mermaid-svg-1749529562038-tp0jz51sh .edgeLabel rect{opacity:0.5;background-color:#1a1a1a99;fill:#1a1a1a99;}#mermaid-svg-1749529562038-tp0jz51sh .labelBkg{background-color:rgba(26, 26, 26, 0.5);}#mermaid-svg-1749529562038-tp0jz51sh .cluster rect{fill:rgba(64, 64, 64, 0.47);stroke:#30373a;stroke-width:1px;}#mermaid-svg-1749529562038-tp0jz51sh .cluster text{fill:#ffffff;}#mermaid-svg-1749529562038-tp0jz51sh .cluster span,#mermaid-svg-1749529562038-tp0jz51sh p{color:#ffffff;}#mermaid-svg-1749529562038-tp0jz51sh div.mermaidTooltip{position:absolute;text-align:center;max-width:200px;padding:2px;font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:12px;background:#88c0d0;border:1px solid #30373a;border-radius:2px;pointer-events:none;z-index:100;}#mermaid-svg-1749529562038-tp0jz51sh .flowchartTitleText{text-anchor:middle;font-size:18px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749529562038-tp0jz51sh :root{--mermaid-font-family:"trebuchet ms",verdana,arial,sans-serif;}</style><g><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="6" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 0 L 10 5 L 0 10 z"/></marker><marker orient="auto" markerHeight="12" markerWidth="12" markerUnits="userSpaceOnUse" refY="5" refX="4.5" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointStart"><path style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 0 5 L 10 10 L 10 0 z"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="11" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-circleEnd"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5" refX="-1" viewBox="0 0 10 10" class="marker flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-circleStart"><circle style="stroke-width: 1; stroke-dasharray: 1, 0;" class="arrowMarkerPath" r="5" cy="5" cx="5"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="12" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-crossEnd"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><marker orient="auto" markerHeight="11" markerWidth="11" markerUnits="userSpaceOnUse" refY="5.2" refX="-1" viewBox="0 0 11 11" class="marker cross flowchart" id="mermaid-svg-1749529562038-tp0jz51sh_flowchart-crossStart"><path style="stroke-width: 2; stroke-dasharray: 1, 0;" class="arrowMarkerPath" d="M 1,1 l 9,9 M 10,1 l -9,9"/></marker><g class="root"><g class="clusters"/><g class="edgePaths"><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-A LE-B" id="L-A-B-0" d="M198.067,33.6L198.067,37.767C198.067,41.933,198.067,50.267,198.067,57.717C198.067,65.167,198.067,71.733,198.067,75.017L198.067,78.3"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-B LE-C1" id="L-B-C1-0" d="M248.18,117.2L260.608,121.367C273.037,125.533,297.894,133.867,310.323,141.317C322.752,148.767,322.752,155.333,322.752,158.617L322.752,161.9"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-B LE-C2" id="L-B-C2-0" d="M156.139,117.2L145.74,121.367C135.341,125.533,114.543,133.867,104.144,141.317C93.745,148.767,93.745,155.333,93.745,158.617L93.745,161.9"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C2 LE-D" id="L-C2-D-0" d="M93.745,200.8L93.745,204.967C93.745,209.133,93.745,217.467,93.745,224.917C93.745,232.367,93.745,238.933,93.745,242.217L93.745,245.5"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C1 LE-E" id="L-C1-E-0" d="M322.752,200.8L322.752,204.967C322.752,209.133,322.752,217.467,321.109,225.006C319.466,232.545,316.18,239.29,314.537,242.663L312.894,246.035"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-C2 LE-E" id="L-C2-E-0" d="M169.947,200.8L188.846,204.967C207.745,209.133,245.543,217.467,265.975,224.996C286.406,232.526,289.471,239.251,291.003,242.614L292.536,245.977"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-E LE-F" id="L-E-F-0" d="M302.389,284.4L302.389,288.567C302.389,292.733,302.389,301.067,302.389,308.517C302.389,315.967,302.389,322.533,302.389,325.817L302.389,329.1"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-G1" id="L-F-G1-0" d="M245.633,362.571L220.318,367.642C195.004,372.714,144.374,382.857,119.06,391.212C93.745,399.567,93.745,406.133,93.745,409.417L93.745,412.7"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-G2" id="L-F-G2-0" d="M302.389,368L302.389,372.167C302.389,376.333,302.389,384.667,302.389,392.117C302.389,399.567,302.389,406.133,302.389,409.417L302.389,412.7"/><path marker-end="url(#mermaid-svg-1749529562038-tp0jz51sh_flowchart-pointEnd)" style="fill:none;" class="edge-thickness-normal edge-pattern-solid flowchart-link LS-F LE-G3" id="L-F-G3-0" d="M359.145,362.929L383.397,367.941C407.648,372.953,456.151,382.976,480.402,391.272C504.653,399.567,504.653,406.133,504.653,409.417L504.653,412.7"/></g><g class="edgeLabels"><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g></g><g class="nodes"><g transform="translate(198.06719207763672, 16.80000114440918)" id="flowchart-A-73" class="node default default flowchart-label"><rect height="33.60000038146973" width="115.87501525878906" y="-16.800000190734863" x="-57.93750762939453" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-50.43750762939453, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="100.87501525878906"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Billing Service</span></div></foreignObject></g></g><g transform="translate(198.06719207763672, 100.40000343322754)" id="flowchart-B-74" class="node default default flowchart-label"><rect height="33.60000038146973" width="157.57501220703125" y="-16.800000190734863" x="-78.78750610351562" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-71.28750610351562, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="142.57501220703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateAllCharges</span></div></foreignObject></g></g><g transform="translate(322.7515640258789, 184.0000057220459)" id="flowchart-C1-76" class="node default default flowchart-label"><rect height="33.60000038146973" width="177.30938720703125" y="-16.800000190734863" x="-88.65469360351562" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-81.15469360351562, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="162.30938720703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateFixedCharges</span></div></foreignObject></g></g><g transform="translate(93.74531555175781, 184.0000057220459)" id="flowchart-C2-78" class="node default default flowchart-label"><rect height="33.60000038146973" width="180.703125" y="-16.800000190734863" x="-90.3515625" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-82.8515625, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="165.703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateUsageCharges</span></div></foreignObject></g></g><g transform="translate(93.74531555175781, 267.60000801086426)" id="flowchart-D-80" class="node default default flowchart-label"><rect height="33.60000038146973" width="187.49063110351562" y="-16.800000190734863" x="-93.74531555175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-86.24531555175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="172.49063110351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">GetUsageBySubscription</span></div></foreignObject></g></g><g transform="translate(302.3890686035156, 267.60000801086426)" id="flowchart-E-83" class="node default default flowchart-label"><rect height="33.60000038146973" width="107.09062957763672" y="-16.800000190734863" x="-53.54531478881836" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-46.04531478881836, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="92.09062957763672"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Price Service</span></div></foreignObject></g></g><g transform="translate(302.3890686035156, 351.2000102996826)" id="flowchart-F-85" class="node default default flowchart-label"><rect height="33.60000038146973" width="113.51250457763672" y="-16.800000190734863" x="-56.75625228881836" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-49.25625228881836, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="98.51250457763672"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CalculateCost</span></div></foreignObject></g></g><g transform="translate(93.74531555175781, 434.800012588501)" id="flowchart-G1-87" class="node default default flowchart-label"><rect height="33.60000038146973" width="159.046875" y="-16.800000190734863" x="-79.5234375" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-72.0234375, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="144.046875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Flat Fee Calculation</span></div></foreignObject></g></g><g transform="translate(302.3890686035156, 434.800012588501)" id="flowchart-G2-89" class="node default default flowchart-label"><rect height="33.60000038146973" width="158.24063110351562" y="-16.800000190734863" x="-79.12031555175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-71.62031555175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="143.24063110351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Package Calculation</span></div></foreignObject></g></g><g transform="translate(504.65313720703125, 434.800012588501)" id="flowchart-G3-91" class="node default default flowchart-label"><rect height="33.60000038146973" width="146.28750610351562" y="-16.800000190734863" x="-73.14375305175781" ry="0" rx="0" style="" class="basic label-container"/><g transform="translate(-65.64375305175781, -9.300000190734863)" style="" class="label"><rect/><foreignObject height="18.600000381469727" width="131.28750610351562"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Tiered Calculation</span></div></foreignObject></g></g></g></g></g></svg>


## 3. API Endpoints

### 3.1 Cost Sheet Management

```http
POST /api/v1/cost_sheets
{
    "meter_id": "string",
    "price_id": "string"
}

GET /api/v1/cost_sheets?tenant_id=string&environment_id=string&status=published

GET /api/v1/cost_sheets/{id}

PUT /api/v1/cost_sheets/{id}
{
    "status": "string"
}

DELETE /api/v1/cost_sheets/{id}
```

### 3.2 Cost Calculation

```http
GET /api/v1/cost_breakdown
{
    "start_time": "timestamp",
    "end_time": "timestamp"
}

Response:
{
    "total_cost": "decimal",
    "items": [
        {
            "meter_id": "string",
            "meter_name": "string",
            "usage": "decimal",
            "cost": "decimal"
        }
    ]
}
```

## 4. Implementation Details

### 4.1 Core Components

1. **Cost Sheet Service**
   - Manages cost sheet lifecycle (CRUD operations)
   - Handles tenant and environment context
   - Validates meter and price associations

2. **Cost Calculation Engine**
   - Integrates with usage tracking
   - Applies price configurations
   - Aggregates costs across meters

### 4.2 Multi-tenancy & Environment Support

1. **Context Management**
   - Automatic tenant and environment extraction from context
   - Enforced isolation between tenants and environments

2. **Data Access**
   - Filtered queries based on tenant_id and environment_id
   - Unique constraints per tenant-environment combination

## 5. Security & Access Control

1. **Data Isolation**
   - Strict tenant and environment boundaries
   - No cross-tenant data access

2. **Status Management**
   - Published/Draft status tracking
   - Unique constraints only on published records







### _Visuals_
## _Detailed function map_


<svg aria-roledescription="classDiagram" role="graphics-document document" viewBox="0 0 860.9609375 716" style="max-width: 860.9609375px;" xmlns="http://www.w3.org/2000/svg" width="100%" id="mermaid-svg-1749564052977-m773awpm2"><style>#mermaid-svg-1749564052977-m773awpm2{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749564052977-m773awpm2 .error-icon{fill:#bf616a;}#mermaid-svg-1749564052977-m773awpm2 .error-text{fill:#bf616a;stroke:#bf616a;}#mermaid-svg-1749564052977-m773awpm2 .edge-thickness-normal{stroke-width:2px;}#mermaid-svg-1749564052977-m773awpm2 .edge-thickness-thick{stroke-width:3.5px;}#mermaid-svg-1749564052977-m773awpm2 .edge-pattern-solid{stroke-dasharray:0;}#mermaid-svg-1749564052977-m773awpm2 .edge-pattern-dashed{stroke-dasharray:3;}#mermaid-svg-1749564052977-m773awpm2 .edge-pattern-dotted{stroke-dasharray:2;}#mermaid-svg-1749564052977-m773awpm2 .marker{fill:rgba(204, 204, 204, 0.87);stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749564052977-m773awpm2 .marker.cross{stroke:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749564052977-m773awpm2 svg{font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:16px;}#mermaid-svg-1749564052977-m773awpm2 g.classGroup text{fill:#2a2a2a;stroke:none;font-family:"trebuchet ms",verdana,arial,sans-serif;font-size:10px;}#mermaid-svg-1749564052977-m773awpm2 g.classGroup text .title{font-weight:bolder;}#mermaid-svg-1749564052977-m773awpm2 .nodeLabel,#mermaid-svg-1749564052977-m773awpm2 .edgeLabel{color:#d8dee9;}#mermaid-svg-1749564052977-m773awpm2 .edgeLabel .label rect{fill:#1a1a1a;}#mermaid-svg-1749564052977-m773awpm2 .label text{fill:#d8dee9;}#mermaid-svg-1749564052977-m773awpm2 .edgeLabel .label span{background:#1a1a1a;}#mermaid-svg-1749564052977-m773awpm2 .classTitle{font-weight:bolder;}#mermaid-svg-1749564052977-m773awpm2 .node rect,#mermaid-svg-1749564052977-m773awpm2 .node circle,#mermaid-svg-1749564052977-m773awpm2 .node ellipse,#mermaid-svg-1749564052977-m773awpm2 .node polygon,#mermaid-svg-1749564052977-m773awpm2 .node path{fill:#1a1a1a;stroke:#2a2a2a;stroke-width:1px;}#mermaid-svg-1749564052977-m773awpm2 .divider{stroke:#2a2a2a;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 g.clickable{cursor:pointer;}#mermaid-svg-1749564052977-m773awpm2 g.classGroup rect{fill:#1a1a1a;stroke:#2a2a2a;}#mermaid-svg-1749564052977-m773awpm2 g.classGroup line{stroke:#2a2a2a;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 .classLabel .box{stroke:none;stroke-width:0;fill:#1a1a1a;opacity:0.5;}#mermaid-svg-1749564052977-m773awpm2 .classLabel .label{fill:#2a2a2a;font-size:10px;}#mermaid-svg-1749564052977-m773awpm2 .relation{stroke:rgba(204, 204, 204, 0.87);stroke-width:1;fill:none;}#mermaid-svg-1749564052977-m773awpm2 .dashed-line{stroke-dasharray:3;}#mermaid-svg-1749564052977-m773awpm2 .dotted-line{stroke-dasharray:1 2;}#mermaid-svg-1749564052977-m773awpm2 #compositionStart,#mermaid-svg-1749564052977-m773awpm2 .composition{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #compositionEnd,#mermaid-svg-1749564052977-m773awpm2 .composition{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #dependencyStart,#mermaid-svg-1749564052977-m773awpm2 .dependency{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #dependencyStart,#mermaid-svg-1749564052977-m773awpm2 .dependency{fill:rgba(204, 204, 204, 0.87)!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #extensionStart,#mermaid-svg-1749564052977-m773awpm2 .extension{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #extensionEnd,#mermaid-svg-1749564052977-m773awpm2 .extension{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #aggregationStart,#mermaid-svg-1749564052977-m773awpm2 .aggregation{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #aggregationEnd,#mermaid-svg-1749564052977-m773awpm2 .aggregation{fill:transparent!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #lollipopStart,#mermaid-svg-1749564052977-m773awpm2 .lollipop{fill:#1a1a1a!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 #lollipopEnd,#mermaid-svg-1749564052977-m773awpm2 .lollipop{fill:#1a1a1a!important;stroke:rgba(204, 204, 204, 0.87)!important;stroke-width:1;}#mermaid-svg-1749564052977-m773awpm2 .edgeTerminals{font-size:11px;}#mermaid-svg-1749564052977-m773awpm2 .classTitleText{text-anchor:middle;font-size:18px;fill:rgba(204, 204, 204, 0.87);}#mermaid-svg-1749564052977-m773awpm2 :root{--mermaid-font-family:"trebuchet ms",verdana,arial,sans-serif;}</style><g><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker aggregation classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-aggregationStart"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker aggregation classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-aggregationEnd"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker extension classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-extensionStart"><path d="M 1,7 L18,13 V 1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker extension classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-extensionEnd"><path d="M 1,1 V 13 L18,7 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="18" class="marker composition classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-compositionStart"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="1" class="marker composition classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-compositionEnd"><path d="M 18,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="6" class="marker dependency classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyStart"><path d="M 5,7 L9,13 L1,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="28" markerWidth="20" refY="7" refX="13" class="marker dependency classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyEnd"><path d="M 18,7 L9,13 L14,7 L9,1 Z"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="13" class="marker lollipop classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-lollipopStart"><circle r="6" cy="7" cx="7" fill="transparent" stroke="black"/></marker></defs><defs><marker orient="auto" markerHeight="240" markerWidth="190" refY="7" refX="1" class="marker lollipop classDiagram" id="mermaid-svg-1749564052977-m773awpm2_classDiagram-lollipopEnd"><circle r="6" cy="7" cx="7" fill="transparent" stroke="black"/></marker></defs><g class="root"><g class="clusters"/><g class="edgePaths"><path marker-end="url(#mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyEnd)" style="fill:none" class="edge-pattern-solid relation" id="id1" d="M226.996,133L216.342,137.167C205.688,141.333,184.379,149.667,173.725,157C163.07,164.333,163.07,170.667,163.07,173.833L163.07,177"/><path marker-end="url(#mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyEnd)" style="fill:none" class="edge-pattern-solid relation" id="id2" d="M546.625,133L557.279,137.167C567.934,141.333,589.242,149.667,599.896,164.5C610.551,179.333,610.551,200.667,610.551,211.333L610.551,222"/><path marker-end="url(#mermaid-svg-1749564052977-m773awpm2_classDiagram-dependencyEnd)" style="fill:none" class="edge-pattern-solid relation" id="id3" d="M163.07,398L163.07,402.167C163.07,406.333,163.07,414.667,163.07,422C163.07,429.333,163.07,435.667,163.07,438.833L163.07,442"/></g><g class="edgeLabels"><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g><g class="edgeLabel"><g transform="translate(0, 0)" class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="edgeLabel"></span></div></foreignObject></g></g></g><g class="nodes"><g transform="translate(163.0703125, 578)" id="classId-costsheet-8" class="node default"><rect height="260" width="249.65625" y="-130" x="-124.828125" class="outer title-state"/><line y2="-99.5" y1="-99.5" x2="124.828125" x1="-124.828125" class="divider"/><line y2="96.5" y1="96.5" x2="124.828125" x1="-124.828125" class="divider"/><g class="label"><foreignObject height="0" width="0"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel"></span></div></foreignObject><foreignObject transform="translate( -54.109375, -122.5)" height="18.5" width="108.21875" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">costsheet</span></div></foreignObject><foreignObject transform="translate( -117.328125, -88)" height="18.5" width="67.8515625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+ID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, -65.5)" height="18.5" width="109.21875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+MeterID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, -43)" height="18.5" width="103.453125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+PriceID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, -20.5)" height="18.5" width="97.59375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Status string</span></div></foreignObject><foreignObject transform="translate( -117.328125, 2)" height="18.5" width="116.109375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+TenantID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, 24.5)" height="18.5" width="158.203125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+EnvironmentID string</span></div></foreignObject><foreignObject transform="translate( -117.328125, 47)" height="18.5" width="159.8828125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CreatedAt time.Time</span></div></foreignObject><foreignObject transform="translate( -117.328125, 69.5)" height="18.5" width="163.5703125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+UpdatedAt time.Time</span></div></foreignObject><foreignObject transform="translate( -117.328125, 104)" height="18.5" width="234.65625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+ToDTO() : *types.costsheet</span></div></foreignObject></g></g><g transform="translate(163.0703125, 290.5)" id="classId-Repository-9" class="node default"><rect height="215" width="310.140625" y="-107.5" x="-155.0703125" class="outer title-state"/><line y2="-54.5" y1="-54.5" x2="155.0703125" x1="-155.0703125" class="divider"/><line y2="-38.5" y1="-38.5" x2="155.0703125" x1="-155.0703125" class="divider"/><g class="label"><foreignObject transform="translate( -41.171875, -100)" height="18.5" width="82.34375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">«interface»</span></div></foreignObject><foreignObject transform="translate( -39.890625, -77.5)" height="18.5" width="79.78125" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Repository</span></div></foreignObject><foreignObject transform="translate( -147.5703125, -31)" height="18.5" width="185.5078125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Create(ctx, item) : error</span></div></foreignObject><foreignObject transform="translate( -147.5703125, -8.5)" height="18.5" width="260.7890625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Get(ctx, id)(*costsheet, error)</span></div></foreignObject><foreignObject transform="translate( -147.5703125, 14)" height="18.5" width="295.140625"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+List(ctx, filter)([]*costsheet, error)</span></div></foreignObject><foreignObject transform="translate( -147.5703125, 36.5)" height="18.5" width="209.6484375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Count(ctx, filter)(int, error)</span></div></foreignObject><foreignObject transform="translate( -147.5703125, 59)" height="18.5" width="189.1953125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Update(ctx, item) : error</span></div></foreignObject><foreignObject transform="translate( -147.5703125, 81.5)" height="18.5" width="165.1328125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+Delete(ctx, id) : error</span></div></foreignObject></g></g><g transform="translate(386.810546875, 70.5)" id="classId-CostSheetService-10" class="node default"><rect height="125" width="480.21875" y="-62.5" x="-240.109375" class="outer title-state"/><line y2="-9.5" y1="-9.5" x2="240.109375" x1="-240.109375" class="divider"/><line y2="6.5" y1="6.5" x2="240.109375" x1="-240.109375" class="divider"/><g class="label"><foreignObject transform="translate( -41.171875, -55)" height="18.5" width="82.34375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">«interface»</span></div></foreignObject><foreignObject transform="translate( -64.890625, -32.5)" height="18.5" width="129.78125" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">CostSheetService</span></div></foreignObject><foreignObject transform="translate( -232.609375, 14)" height="18.5" width="465.21875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+GetInputCostForMargin(ctx, req)(*GetInputCostResponse, error)</span></div></foreignObject><foreignObject transform="translate( -232.609375, 36.5)" height="18.5" width="440.9921875"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CalculateMargin(totalCost, totalRevenue) : decimal.Decimal</span></div></foreignObject></g></g><g transform="translate(610.55078125, 290.5)" id="classId-PriceService-11" class="node default"><rect height="125" width="484.8203125" y="-62.5" x="-242.41015625" class="outer title-state"/><line y2="-9.5" y1="-9.5" x2="242.41015625" x1="-242.41015625" class="divider"/><line y2="6.5" y1="6.5" x2="242.41015625" x1="-242.41015625" class="divider"/><g class="label"><foreignObject transform="translate( -41.171875, -55)" height="18.5" width="82.34375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">«interface»</span></div></foreignObject><foreignObject transform="translate( -46.84375, -32.5)" height="18.5" width="93.6875" class="classTitle"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">PriceService</span></div></foreignObject><foreignObject transform="translate( -234.91015625, 14)" height="18.5" width="393.984375"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CalculateCost(ctx, price, quantity) : decimal.Decimal</span></div></foreignObject><foreignObject transform="translate( -234.91015625, 36.5)" height="18.5" width="469.8203125"><div style="display: inline-block; white-space: nowrap;" xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">+CalculateCostSheetPrice(ctx, price, quantity) : decimal.Decimal</span></div></foreignObject></g></g></g></g></g></svg>


















