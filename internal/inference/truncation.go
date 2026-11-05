package inference

// cutOff marks an answer that the provider stopped at its length limit. The
// partial text is still useful, so it is returned with a note instead of failing.
func cutOff(text string) string {
	return text + "\n\n_The answer was cut off by the model's length limit. Press s to try again for a shorter one._"
}
