package collect

func ProcessTPSMetrics(timePeriod int, inputOptions ...Option) error {
	opts := DefaultOptions()

	for _, inputOption := range inputOptions {
		inputOption(opts)
	}

	return nil
}
