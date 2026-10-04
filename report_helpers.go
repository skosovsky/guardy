package guardy

// FinishReport applies control-flow defaults and disposition to a manually constructed report.
func FinishReport(rep *Report, spec ControlSpec) *Report {
	if rep == nil {
		return nil
	}
	applyControlDefaults(rep, spec)
	rep.Disposition = rep.effectiveDisposition()
	return rep
}
