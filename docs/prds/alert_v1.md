Alert logs table:
id  // alert id
tenant_id // tenant id
environment_id // environment id
entity_type, 
entity_id // 
alert_type 
alert_status // ok or in_alarm
alert_info // alert info (jsonb) sort of like metadata, this will hold the alert information namely- threshold, value_at_time, timestamp
created_at // created at
updated_at // updated at
created_by // created by 
updated_by // updated by


Now there can be any method via which the alert would be detected now or in the near future
- cron
- flag check

This Alert Engine will work as:
via any method when breach is detected then 
    check alert for the entity.id x alert_type in alert logs table (pull the latest row) List with limit 1
    if entity.id alert log exists
        then if determined alert status i.e alert_state = in_alarm is same as existing db log alert alert_state = in_alarm
            then we will skip
        else 
            create a new entry in alert logs table with alert status in_alarm
            fire the relevant alert webhook which is basically notification channel
    else
        create a new entry in alert logs table with alert status in_alarm
        fire the relevant alert webhook which is basically notification channel


similarly if when we want to check recovery as well for some entity not all 
with it own means when it is detected that there is no breach
    check alert for the entity.id x alert_type in alert logs table if exists(pull the latest row) List with limit 1
        if entity.id alert log exists
            then if determined alert status i.e alert_state = ok is same as existing db log alert alert_state = ok
                then we will skip
            else 
                create a new entry in alert logs table with alert status ok
                fire the relevant recovery webhook which is basically notification channel
        else 
            create a new entry in alert logs table with alert status ok
            fire the relevant recovery webhook which is basically notification channel

When to log & publish alert:
for publishing alert triggered cond.
 if threshold breached &&
1. if latest alert for the entity.id x alert_type not exists 
2. if latest alert for the entity.id x alert_type exists && alert state is ok

Note: if latest alert for the entity.id x alert_type exists && alert state is in_alarm then:
no logging -> dont publish shit 

for publishing alert recovered cond.
if threshold not breached &&
1. if latest alert for the entity.id x alert_type exists && alert state is in_alarm

Note if if latest alert for the entity.id x alert_type not exists or latest alert for the entity.id x alert_type exists && alert state is ok then:
no logging -> dont publish shit 
