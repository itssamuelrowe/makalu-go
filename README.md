## Example

```
{
    "id": 1,
    "name": "Everest",
    "first_ascent_by": "Edmund Hillary"
}
```

## Operators

### $is -- IS TYPE operator

Example:
```
target: GET ${host}/mountains/1
out:
  id: $number
  name:
    $is: $string
  first_ascent_by: $string
```

### $is_not -- IS NOT TYPE operator

Example:
```
target: GET ${host}/mountains/1
out:
  id: $number
  name:
    $is_not: $number
  first_ascent_by: $string
```

### $ne -- NOT EQUALS operator

Example:
```
target: GET ${host}/mountains/1
out:
  id: $number
  name: $string
  first_ascent_by:
    $ne: George Mallory
```

### $regex -- REGEX MATCHES operator

Example:
```
target: GET ${host}/mountains/1
out:
  id: $number
  name:
    $regex: "(Mount Everest)|(Sagarmatha)"
  first_ascent_by: $string
```
