table "state" {
  schema = schema.main
  column "id" {
    null = false
    type = integer
  }
  column "data" {
    null = false
    type = text
  }
  column "checksum" {
    null = false
    type = blob
  }
  column "updated_at" {
    null = false
    type = integer
  }
  primary_key {
    columns = [column.id]
  }
}

table "state" {
  schema = schema.main
  column "id" {
    null = false
    type = integer
  }
  column "data" {
    null = false
    type = text
  }
  column "checksum" {
    null = false
    type = blob
  }
  column "updated_at" {
    null = false
    type = integer
  }
  primary_key {
    columns = [column.id]
  }
  check "id_check" {
    expr = "id = 1"
  }
}

schema "main" {
}

