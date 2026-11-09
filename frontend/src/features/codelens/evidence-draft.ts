import { create } from 'zustand';
import type { ContextCitation } from '@/lib/api';

interface ArtifactDraft {
  citation: ContextCitation;
  projectPath: string;
}

interface EvidenceDraftState {
  artifact: ArtifactDraft | null;
  attach: (artifact: ArtifactDraft) => void;
  remove: (artifact: ArtifactDraft) => void;
}

export const useEvidenceDraft = create<EvidenceDraftState>((set) => ({
  artifact: null,
  attach: artifact => set({ artifact }),
  remove: artifact => set(state => state.artifact === artifact ? { artifact: null } : state),
}));
