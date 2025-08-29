#### Subscription Upgrade and Downgrade Workflow


##### Is this Behaviour correct ?
1. Upgrade will take place immediately
2. Downgrade should take place in the period end
3. Unused charges should be given as credit grant


##### Is this User flow correct ?
1. user is allowed to change the plan in subscription.
2. He sees all the preview before confirming the subscription
3. He executes the change 
4. A invoice is genreated for prorated charges and new plan advance charges if there are any.


##### Should we keep this Limitation ? 
2. Users are not allowd to change the currency of subscription.
3. Users are not allowed to change the billing cycle.


##### Coupons handling
1. Even if the plan is changing the coupon should proceed.
2. What should happen to line item level coupons ?
3. What should happen to price overrides over line items ?
4. What should happen to addons ?  



#### Charges
1. Usage based charges are tricky to handle. Suppose a susbcription plan is updated which has usage based charge. Now the invoice is immediaetly billed for fixed charges, so should be bill immediately for usagae based charge too. Then in nwxt invoice we should only considert he usage after upgradation.

#### Line items
1. Suppose new plan doesn not have same line item, so what happens to coupon, should be discard line item coupons. For now just discard the coupons which are line item level.



##### What next steps should be.
1. Create a implementation doc where we address every touch point and edge cases.   
    1. Do not add any code snippet here and do not change any files.
    2. Lets skip the subscription versioning also
    3. Point out each edge case in very detailed
