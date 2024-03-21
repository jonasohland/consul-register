service {
  name = "blub"
  check {
    name                              = "Still Alive"
    http                              = "http://127.0.0.1:8082"
    deregister_critical_service_after = "15s"
    interval                          = "4s"
  }
}