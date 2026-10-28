package backup

// BeforeRead runs hook with each file's name after the listing and before the file is
// opened, so a test can change the directory in between.
func BeforeRead(s *Sensor, hook func(name string)) { s.beforeRead = hook }
