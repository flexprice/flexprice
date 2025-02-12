# Defining features

1. Defining Billable metric:
    1. Event name : tokens_total
    2. key : model
    3. values : gpt 4o, 4o-mini
    
2. Features
    1. Boolean
        1. yes/no
        e.g. - 
        Basic plan don’t have an access to Auth
        Pro plan have an access to Auth
            1. Feature Name : Auth
            2. Feature type: Boolean
            
    2. Metered
        1. give access to a customer by imposing some limits
        e.g. -
        Limited access to o1 and o1-mini (in case of chatgpt)
        Basic plan has an access to gpt 4o model upto 1M tokens
        Pro plan has access to unlimited gpt 4o model 
            1. Feature name: GPT 4o model
            2. Feature type: Metered
            3. Meter type : tokens_total
            4. Filter: GPT 4o
            
        2. if billing cycle of a plan is yearly: 
            1. can entitlements be offered at month level?
                
                ![Screenshot 2025-02-04 at 5.40.57 PM.png](Defining%20features%201909b3a59a6880979e33d1c0d256bed8/Screenshot_2025-02-04_at_5.40.57_PM.png)
                
            2. how to know whether entitlements will be offered per month or per year?
            
        3. preserve usage/ overages
            1. i purchased annual basic plan 
            2. i have used 3k pageviews in the 1st month
            3. for next month, can remaining 2k pageviews gets forwarded (i.e. unused values of previous month)
            
        4. defining usage priority
        If a system is metered by token on usage, then as part of their subscription each customer gets 10.000 tokens/month. Certian users require more tokens than this, so we are granting them an additional 100.000 tokens/year for extra fees.
        We would want the customer to first use their available balance from the 10.000 tokens/month allowed balance, and if they have used all of that, then they should start using the 100.000 tokens/year balance.
        This can be achieved by creating two grants:
            - Grant 1: 10.000 tokens that rolls over each month with the usage period, priority=5
            - Grant 2: 100.000 tokens recurring each year, priority=10
                - First, grants with higher priority are burnt down before grants with lower priority.
                1. In case of grants with the same priority, the grant that is closest to its expiration date is burnt down first.
                2. In case of grants with the same priority and expiration, the grant that was created first is burnt down first.
                
        5. soft limit vs hard limit
        
    3. Config (Custom)
    

|  | Basic plan | Pro plan |
| --- | --- | --- |
| Pricing ( Monthly) | **$10/ month** | **$20/month** |
| Pricing ( Annual) | **$8/ month** | **$18/month** |
| Features |  |  |
| Auth | No | Yes |
| GPT 4o model | 1M tokens | Unlimited |
| Pageviews | 5k per month | 10k per month |
| SLA | Ticket / Email Support 12-24 Hours | Ticket / Email Support
<12 Hours |

Things to be picked up tomorrow

1. Change the copies of the page
2. Create feature tab:
    1. Boolean
    2. Metered - add filter option 
    3. Config