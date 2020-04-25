# Fugitive3dServerRepository
The server back end for Fugitive 3D's server browser

# Responses

Servers registering for the first time are returned a 201/Created with a JSON response of:

```
{
  "result": "created"
}

When they register again, if it's within 60 seconds of the last update, they are returned a 200/OK with a JSON response of:

```
{
  "result": "updated"
}
```