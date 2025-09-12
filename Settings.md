#### Settings Revamp
I want to revamp the settings flow inside the current system. 
I want to reimagine the validation flow and make it simple without. 


##### PROBLEM 
The validation flow is passes to layers and makes it complex. 


##### GOAL
Goal is to make the settings simpler.


##### NOTES
Think like a Google Enginner to revamp the whole flow. Dont change anything


#### WHAT approach I am thinking

1. Types 
   - Settings
     - invoice config.go
       - Struct
       - Validator

2. DTO
   - UpsertSettingsRequest
     - Based on key figure out the Type
     - Convert to the type


3. Service:
   - UpsertSettingsByKey
     - Validate the setting type
     - if existing:
        - Update the whole
     - else:
        - Create new