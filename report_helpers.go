package guardy

// FinishReport applies control-flow defaults to a manually constructed report.
func FinishReport(rep *Report, spec ControlSpec) *Report {
	if rep == nil {
		return nil
	}
	ApplyControlDefaults(rep, spec)
	return rep
}
